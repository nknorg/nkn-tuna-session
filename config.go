package session

import (
	"github.com/imdario/mergo"
	"github.com/nknorg/ncp"
)

type Config struct {
	NumTunaListeners int
	TunaMaxPrice     string
	TunaDialTimeout  int // in millisecond
	SessionConfig    *SessionConfig
}

var defaultConfig = Config{
	NumTunaListeners: 4,
	TunaMaxPrice:     "0",
	TunaDialTimeout:  10000,
	SessionConfig:    nil,
}

func DefaultConfig() *Config {
	conf := defaultConfig
	conf.SessionConfig = DefaultSessionConfig()
	return &conf
}

var defaultSessionConfig = SessionConfig{
	NonStream:                    false,
	SessionWindowSize:            4 << 20,
	MTU:                          1300,
	InitialConnectionWindowSize:  16,
	MaxConnectionWindowSize:      256,
	MinConnectionWindowSize:      1,
	MaxAckSeqListSize:            32,
	FlushInterval:                10,
	Linger:                       1000,
	InitialRetransmissionTimeout: 5000,
	MaxRetransmissionTimeout:     10000,
	SendAckInterval:              50,
	CheckTimeoutInterval:         50,
	DialTimeout:                  0,
}

func DefaultSessionConfig() *SessionConfig {
	sessionConf := defaultSessionConfig
	return &sessionConf
}

type SessionConfig ncp.Config

func MergedConfig(conf *Config) (*Config, error) {
	merged := DefaultConfig()
	if conf != nil {
		err := mergo.Merge(merged, conf, mergo.WithOverride)
		if err != nil {
			return nil, err
		}
	}
	return merged, nil
}

func MergeSessionConfig(base, conf *SessionConfig) (*SessionConfig, error) {
	merged := *base
	if conf != nil {
		err := mergo.Merge(&merged, conf, mergo.WithOverride)
		if err != nil {
			return nil, err
		}
	}
	return &merged, nil
}
