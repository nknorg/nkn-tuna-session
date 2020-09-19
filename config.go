package session

import (
	"github.com/imdario/mergo"
	ncp "github.com/nknorg/ncp-go"
	"github.com/nknorg/tuna"
	"github.com/nknorg/tuna/geo"
)

type Config struct {
	NumTunaListeners       int
	TunaDialTimeout        int // in millisecond
	TunaMaxPrice           string
	TunaNanoPayFee         string
	TunaServiceName        string
	TunaSubscriptionPrefix string
	TunaIPFilter           *geo.IPFilter
	SessionConfig          *ncp.Config
}

var defaultConfig = Config{
	NumTunaListeners:       4,
	TunaDialTimeout:        10000,
	TunaMaxPrice:           "0",
	TunaNanoPayFee:         "0",
	TunaServiceName:        tuna.DefaultReverseServiceName,
	TunaSubscriptionPrefix: tuna.DefaultSubscriptionPrefix,
	TunaIPFilter:           nil,
	SessionConfig:          nil,
}

func DefaultConfig() *Config {
	conf := defaultConfig
	conf.TunaIPFilter = &geo.IPFilter{}
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
