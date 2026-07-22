package steam

type DownloadUpdate struct {
	Type string `json:"type"` // "download-update"
	Data []struct {
		ID              string  `json:"id"`
		Name            string  `json:"name"`
		Status          string  `json:"status"`
		Progress        float64 `json:"progress"`
		BytesDownloaded int64   `json:"current"`
		BytesTotal      int64   `json:"total"`
	} `json:"data"`
	Meta struct {
		Connected bool   `json:"connected"`
		Name      string `json:"name"`
	} `json:"meta"`
}

type Command struct {
	ID      string      `json:"id,omitempty"`
	Type    string      `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

type Response struct {
	ID     string      `json:"id"`
	Type   string      `json:"type"`
	Result interface{} `json:"result"`
}
