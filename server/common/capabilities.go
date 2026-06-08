package common

import (
	"encoding/json"
	"os"
	"runtime"
)

type EncoderCapability struct {
	SupportedServers []string `json:"supported_servers"`
	SupportedClients []string `json:"supported_clients"`
	Chroma           []string `json:"chroma"`
	Lossless         bool     `json:"lossless"`
}

type CodecCapability struct {
	Description string                       `json:"description"`
	Encoders    map[string]EncoderCapability `json:"encoders"`
}

type CapabilitiesSchema struct {
	Codecs map[string]CodecCapability `json:"codecs"`
}

type ValidCombination struct {
	Codec            string   `json:"codec"`
	Encoder          string   `json:"encoder"`
	Chroma           string   `json:"chroma"`
	Lossless         bool     `json:"lossless"`
	SupportedClients []string `json:"supported_clients"`
}

// defaultCapabilitiesJSON acts as a self-contained fallback for portable packaged environments.
const defaultCapabilitiesJSON = `{
  "codecs": {
    "vp8": {
      "description": "Legacy fallback codec, high latency, software only.",
      "encoders": {
        "cpu": {
          "supported_servers": ["linux"],
          "supported_clients": ["browser"],
          "chroma": ["420"],
          "lossless": false
        }
      }
    },
    "h264": {
      "description": "Standard high compatibility codec.",
      "encoders": {
        "cpu": {
          "supported_servers": ["linux"],
          "supported_clients": ["browser", "wayland", "macos"],
          "chroma": ["420", "444"],
          "lossless": false
        },
        "macos": {
          "supported_servers": ["macos"],
          "supported_clients": ["browser", "wayland", "macos"],
          "chroma": ["420", "444"],
          "lossless": false
        },
        "nvenc": {
          "supported_servers": ["linux"],
          "supported_clients": ["browser", "wayland", "macos"],
          "chroma": ["420", "444"],
          "lossless": true
        },
        "intel": {
          "supported_servers": ["linux"],
          "supported_clients": ["browser", "wayland", "macos"],
          "chroma": ["420"],
          "lossless": false
        }
      }
    },
    "h265": {
      "description": "High Efficiency Video Coding (HEVC).",
      "encoders": {
        "cpu": {
          "supported_servers": ["linux"],
          "supported_clients": ["browser", "macos", "wayland"],
          "chroma": ["420", "444"],
          "lossless": false
        },
        "macos": {
          "supported_servers": ["macos"],
          "supported_clients": ["browser", "macos", "wayland"],
          "chroma": ["420", "444"],
          "lossless": false
        },
        "nvenc": {
          "supported_servers": ["linux"],
          "supported_clients": ["browser", "wayland", "macos"],
          "chroma": ["420", "444"],
          "lossless": true
        },
        "intel": {
          "supported_servers": ["linux"],
          "supported_clients": ["browser", "wayland", "macos"],
          "chroma": ["420", "444"],
          "lossless": false
        }
      }
    },
    "av1": {
      "description": "AOMedia Video 1, excellent compression at low bitrates.",
      "encoders": {
        "cpu": {
          "supported_servers": ["linux"],
          "supported_clients": ["browser", "wayland", "macos"],
          "chroma": ["420"],
          "lossless": false
        },
        "nvenc": {
          "supported_servers": ["linux"],
          "supported_clients": ["browser", "wayland", "macos"],
          "chroma": ["420"],
          "lossless": false
        },
        "intel": {
          "supported_servers": ["linux"],
          "supported_clients": ["browser", "wayland", "macos"],
          "chroma": ["420"],
          "lossless": false
        }
      }
    }
  }
}`

// GetValidCombinations filters and returns valid codec/encoder/chroma combinations.
func GetValidCombinations() []ValidCombination {
	var combos []ValidCombination

	// 1. Identify server platform
	serverOS := "linux"
	if runtime.GOOS == "darwin" {
		serverOS = "macos"
	}

	// 2. Load and parse capabilities mapping
	var schema CapabilitiesSchema
	data, err := os.ReadFile("capabilities.jsonc")
	if err != nil {
		data, err = os.ReadFile("../capabilities.jsonc")
	}
	if err != nil {
		data = []byte(defaultCapabilitiesJSON)
	}

	if err := json.Unmarshal(data, &schema); err != nil {
		// Fallback parse attempt using static JSON
		_ = json.Unmarshal([]byte(defaultCapabilitiesJSON), &schema)
	}

	// 3. Resolve combinations based on detected server flags
	for codecName, codecInfo := range schema.Codecs {
		for encoderName, encoderInfo := range codecInfo.Encoders {
			// Ensure current server OS is supported
			osSupported := false
			for _, srv := range encoderInfo.SupportedServers {
				if srv == serverOS {
					osSupported = true
					break
				}
			}
			if !osSupported {
				continue
			}

			// In direct capture mode, CPU encoders are not supported
			if CaptureMode == CaptureModeDirect && encoderName == "cpu" {
				continue
			}

			// Verify if the hardware is available based on detected runtime flags
			hardwareOk := false
			switch encoderName {
			case "cpu":
				hardwareOk = true
			case "macos":
				hardwareOk = (serverOS == "macos")
			case "nvenc":
				if UseNVIDIA {
					if codecName == "av1" {
						hardwareOk = AV1NVENCAvailable
					} else {
						hardwareOk = true
					}
				}
			case "intel":
				if UseIntel {
					if codecName == "h265" {
						hardwareOk = H265QSVAvailable
					} else if codecName == "av1" {
						hardwareOk = AV1QSVAvailable
					} else {
						hardwareOk = QSVAvailable
					}
				}
			}

			if !hardwareOk {
				continue
			}

			// Match valid chroma formats
			for _, chr := range encoderInfo.Chroma {
				chromaOk := true
				if chr == "444" {
					if encoderName == "nvenc" {
						if codecName == "h264" {
							chromaOk = H264NVENC444Available
						} else if codecName == "h265" {
							chromaOk = H265NVENC444Available
						} else {
							chromaOk = false
						}
					} else if encoderName == "intel" {
						if codecName == "h265" {
							chromaOk = H265QSVAvailable
						} else {
							chromaOk = false
						}
					}
				}

				if !chromaOk {
					continue
				}

				combos = append(combos, ValidCombination{
					Codec:            codecName,
					Encoder:          encoderName,
					Chroma:           chr,
					Lossless:         encoderInfo.Lossless,
					SupportedClients: encoderInfo.SupportedClients,
				})
			}
		}
	}

	return combos
}
