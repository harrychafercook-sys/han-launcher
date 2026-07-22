package steam

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type Sidecar struct {
	ctx           context.Context
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	stdout        io.ReadCloser
	mu            sync.RWMutex // Changed to RWMutex for better concurrency
	requests      map[string]chan interface{}
	reqID         int
	downloadCache interface{}
	statusCache   interface{}
	restartMu     sync.Mutex // Separate mutex for restart coordination
	isRestarting  bool
}

func NewSidecar() *Sidecar {
	return &Sidecar{
		requests: make(map[string]chan interface{}),
	}
}

// Helper to log to file for production debugging
func (s *Sidecar) logToFile(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	fmt.Println(msg) // Keep console for dev

	configDir, _ := os.UserConfigDir()
	logPath := filepath.Join(configDir, "dayz-launcher-go", "sidecar_debug.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	f.WriteString(fmt.Sprintf("[%s] %s\n", timestamp, msg))
}

func (s *Sidecar) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctx = ctx
	return s.startInternal()
}

// startInternal assumes caller holds s.mu
func (s *Sidecar) startInternal() error {
	// Cleanup existing if any
	if s.cmd != nil && s.cmd.Process != nil {
		s.logToFile("Killing existing sidecar process...")
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()                   // Ensure process is reaped
		time.Sleep(200 * time.Millisecond) // Give OS time to release file handles
	}

	// 1. Determine Sidecar Path
	// In production, we expect "sidecar" folder to be next to the executable
	execPath, err := os.Executable()
	if err != nil {
		return err
	}
	execDir := filepath.Dir(execPath)
	sidecarDir := filepath.Join(execDir, "sidecar")

	// Check if prod path exists
	if _, err := os.Stat(sidecarDir); os.IsNotExist(err) {
		// 2. Dev Mode Fallback
		// Look for backend/sidecar in current directory
		cwd, _ := os.Getwd()
		sidecarDir = filepath.Join(cwd, "backend", "sidecar")
		s.logToFile("Production sidecar not found. Attempting dev path: %s", sidecarDir)
	} else {
		s.logToFile("Found production sidecar: %s", sidecarDir)
	}

	nodeExe := filepath.Join(sidecarDir, "node.exe")
	if _, err := os.Stat(nodeExe); os.IsNotExist(err) {
		s.logToFile("Bundled node.exe not found at %s, using system node.", nodeExe)
		nodeExe = "node"
	}

	s.cmd = exec.Command(nodeExe, "main.js")
	s.cmd.Dir = sidecarDir
	s.cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW

	stdin, err := s.cmd.StdinPipe()
	if err != nil {
		s.logToFile("Failed to create StdinPipe: %v", err)
		return err
	}
	s.stdin = stdin

	stdout, err := s.cmd.StdoutPipe()
	if err != nil {
		s.logToFile("Failed to create StdoutPipe: %v", err)
		return err
	}
	s.stdout = stdout
	s.cmd.Stderr = os.Stderr

	if err := s.cmd.Start(); err != nil {
		s.logToFile("Failed to start sidecar: %v", err)
		return fmt.Errorf("failed to start sidecar: %w", err)
	}

	go s.listen()

	return nil
}

func (s *Sidecar) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopInternal()
}

func (s *Sidecar) stopInternal() error {
	if s.cmd != nil && s.cmd.Process != nil {
		s.logToFile("Stopping Sidecar...")
		if err := s.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill sidecar: %w", err)
		}
		s.cmd = nil
	}
	return nil
}

func (s *Sidecar) Restart() {
	s.restartMu.Lock()
	if s.isRestarting {
		s.restartMu.Unlock()
		return
	}
	s.isRestarting = true
	s.restartMu.Unlock()

	defer func() {
		s.restartMu.Lock()
		s.isRestarting = false
		s.restartMu.Unlock()
	}()

	s.logToFile("[Sidecar] WATCHDOG: Restarting Sidecar Process...")

	s.mu.Lock()
	// Clear all pending requests with error
	for id, ch := range s.requests {
		select {
		case ch <- map[string]interface{}{"success": false, "error": "Sidecar Restarting"}:
		default:
		}
		delete(s.requests, id)
	}

	err := s.startInternal()
	s.mu.Unlock()

	if err != nil {
		s.logToFile("[Sidecar] Restart Failed: %v", err)
		// Retry once after delay?
		time.Sleep(2 * time.Second)
		s.mu.Lock()
		_ = s.startInternal()
		s.mu.Unlock()
	} else {
		s.logToFile("[Sidecar] Restart Successful.")
	}
}

func (s *Sidecar) listen() {
	reader := bufio.NewReader(s.stdout)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			fmt.Println("[Sidecar] Disconnected (Read Error):", err)
			// Trigger Restart if not explicitly stopped (hard to know exact intent without flag, assuming crash)
			// We check if cmd is still non-nil (meaning we didn't call Stop())
			s.mu.Lock()
			shouldRestart := s.cmd != nil
			s.mu.Unlock()

			if shouldRestart {
				go s.Restart()
			}
			return
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(line, &msg); err != nil {
			fmt.Printf("[Sidecar Log] %s", string(line))
			continue
		}

		msgType, _ := msg["type"].(string)

		if msgType == "download-update" {
			if data, ok := msg["data"]; ok {
				s.downloadCache = data
			}
			if meta, ok := msg["meta"]; ok {
				s.statusCache = meta
			}
			runtime.EventsEmit(s.ctx, "download-update", msg)
		} else if id, ok := msg["id"].(string); ok {
			s.mu.Lock()
			if ch, exists := s.requests[id]; exists {
				ch <- msg["result"]
				delete(s.requests, id)
			}
			s.mu.Unlock()
		}
	}
}

func (s *Sidecar) Send(method string, payload interface{}) (interface{}, error) {
	s.mu.Lock()
	id := fmt.Sprintf("%d", s.reqID)
	s.reqID++
	ch := make(chan interface{}, 1)
	s.requests[id] = ch

	cmd := Command{
		ID:      id,
		Type:    method,
		Payload: payload,
	}

	bytes, err := json.Marshal(cmd)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}

	// Optimization: check if stdin is valid
	if s.stdin == nil {
		s.mu.Unlock()
		go s.Restart()
		return nil, fmt.Errorf("sidecar not running")
	}

	if _, err := s.stdin.Write(append(bytes, '\n')); err != nil {
		s.mu.Unlock()
		fmt.Printf("[Sidecar] Write Error: %v. Triggering Restart.\n", err)
		go s.Restart()
		return nil, err
	}
	s.mu.Unlock()

	select {
	case res := <-ch:
		return res, nil
	case <-time.After(10 * time.Second): // 10s Timeout (Allows sidecar 8-9s to timeout safely)
		fmt.Printf("[Sidecar] Timeout waiting for %s (ID: %s). Triggering Restart.\n", method, id)

		s.mu.Lock()
		delete(s.requests, id)
		s.mu.Unlock()

		go s.Restart()
		return nil, fmt.Errorf("timeout awaiting sidecar response")
	}
}

func (s *Sidecar) GetStatus() interface{} {
	return s.statusCache
}

func (s *Sidecar) GetDownloads() interface{} {
	return s.downloadCache
}
