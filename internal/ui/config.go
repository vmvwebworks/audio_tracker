package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds user settings persisted between runs.
type Config struct {
	Driver          string  `json:"driver"`
	Input           int     `json:"input"`
	OutL            int     `json:"out_l"`
	OutR            int     `json:"out_r"`
	LastFile        string  `json:"last_file"`
	LatencyAdjustMs float64 `json:"latency_adjust_ms"`
	Metronome       bool    `json:"metronome"`
	InputGainDb     float64 `json:"input_gain_db"`
	// Monitor sends the input to the output while the record track is armed.
	// Off by default: with speakers instead of headphones it feeds back.
	Monitor bool `json:"monitor"`
	// KeepAudio keeps the audio device open while the window is in the
	// background instead of releasing it for other programs.
	KeepAudio bool `json:"keep_audio_in_background"`
	// TunerA4 is the reference pitch of the tuner in Hz; 0 means 440.
	TunerA4 float64 `json:"tuner_a4"`
	// TunerInstrument is the tuner preset ("" = tuning of the selected track)
	// and TunerCustom the strings the user changed, per preset.
	TunerInstrument string           `json:"tuner_instrument"`
	TunerCustom     map[string][]int `json:"tuner_custom"`
	// Zoom is the score magnification; 0 means 1.
	Zoom float64 `json:"zoom"`
	// CountIn is the number of bars of clicks before a recording starts.
	CountIn int `json:"count_in_bars"`
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "audio_tracker", "config.json")
}

// LoadConfig reads the saved settings, returning defaults if there are none.
func LoadConfig() *Config {
	c := &Config{Driver: "ASIO4ALL v2", OutL: 0, OutR: 1, TunerA4: 440, CountIn: 1}
	if b, err := os.ReadFile(configPath()); err == nil {
		_ = json.Unmarshal(b, c)
	}
	return c
}

// Save writes the settings to disk.
func (c *Config) Save() error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}
