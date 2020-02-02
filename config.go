package session

import (
	"github.com/imdario/mergo"
	ncp "github.com/nknorg/ncp-go"
)

type Config struct {
	NumTunaListeners int
	TunaMaxPrice     string
	TunaDialTimeout  int // in millisecond
	SessionConfig    *ncp.Config
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

var defaultSessionConfig = ncp.Config{
	MTU: 1300,
}

func DefaultSessionConfig() *ncp.Config {
	sessionConf := defaultSessionConfig
	return &sessionConf
}

type DialConfig struct {
	DialTimeout   int32 //in millisecond
	SessionConfig *ncp.Config
}

var defaultDialConfig = DialConfig{
	DialTimeout:   0,
	SessionConfig: nil,
}

func DefaultDialConfig(baseSessionConfig *ncp.Config) *DialConfig {
	dialConf := defaultDialConfig
	sessionConfig := *baseSessionConfig
	dialConf.SessionConfig = &sessionConfig
	return &dialConf
}

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

func MergeDialConfig(baseSessionConfig *ncp.Config, conf *DialConfig) (*DialConfig, error) {
	merged := DefaultDialConfig(baseSessionConfig)
	if conf != nil {
		err := mergo.Merge(merged, conf, mergo.WithOverride)
		if err != nil {
			return nil, err
		}
	}
	return merged, nil
}
