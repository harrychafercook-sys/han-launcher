# Security & Antivirus False Positives Report

## Overview
This document addresses the "False Positive" detections that may appear when scanning the **DayZ Launcher** executable on VirusTotal or with certain antivirus software. 

**Status:** The application is safe. These detections are "heuristics" (guesses) triggered by the technology stack used (Go + Wails), not by malicious code.

## Summary of Detections
You may see flags such as:
- **Avast / AVG:** `Win64:Evo-gen [Trj]`
- **Bkav Pro / Trapmine:** `W64.AIDetectMalware`, `Malicious.moderate.ml.score`
- **Microsoft:** `Program:Win32/Wacapew.C!ml`
- **Sangfor Engine Zero:** `Trojan.Win32.Save.a` (A known AI-based malware detector with a history of frequent false positives).

### Why this happens
1.  **Machine Learning Detections (`!ml`):** The `!ml` suffix in `Wacapew.C!ml` stands for **Machine Learning**. This means a specific virus signature was **not** found. Instead, Microsoft's AI analyzed the program's behavior (downloading files, launching processes) and "guessed" it might be unwanted because it lacks a digital signature and reputation.
2.  **Generic Heuristics:** The tag `Evo-gen` stands for "Evolutionary Generation". It is a generic catch-all for files that the antivirus does not recognize and that have a structure it finds "unusual".
2.  **Go & Wails Architecture:** This application is built using **Go** and **Wails**. Go binaries are large, statically linked files that function differently from standard C++ programs. This unique structure, combined with the embedded WebView2 usage (Wails), often confuses older antivirus heuristics into thinking the file is a "dropper" or "packer".
3.  **Behavioral Flags:** The application performs actions inherent to a game launcher:
    - Scanning directories (to find mod folders).
    - Executing processes (launching `DayZ_x64.exe`).
    - Connecting to the internet (checking server status, Steam authentication, downloading server list from battlemetrics, etc.).
    
    Security tools may flag these as "Discovery" or "Execution" techniques, but they are legitimate and necessary functions for the launcher to work.

## Verification
**[View Latest VirusTotal Scan Report](https://www.virustotal.com/gui/file/8185ea2f5f1b976dca9e450102c3d2288062cfee5b94ba1e6b79a568c04b4a9a)**

The application has been scanned by virtually all major security vendors and found to be **Undetected** (Clean) by industry leaders, including but not limited to:
- Microsoft (Defender)
- CrowdStrike
- SentinelOne
- Kaspersky
- Malwarebytes
- BitDefender
- Sophos

## Actions Taken
We have actively submitted the application binary to the relevant antivirus vendors (Avast, AVG, etc.) for analysis and whitelisting. These false positives generally resolve as the application gains "reputation" or after manual whitelist processing by the vendors.

## Conclusion
If your antivirus flags the launcher:
1.  It is a known false positive affecting many Wails/Go applications.
2.  You can verify the source code yourself on GitHub.
