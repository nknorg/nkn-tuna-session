package session

import (
	"github.com/imdario/mergo"
	ncp "github.com/nknorg/ncp-go"
	"github.com/nknorg/tuna"
	"github.com/nknorg/tuna/filter"
	"github.com/nknorg/tuna/geo"
)

type Config struct {
	NumTunaListeners       int
	TunaDialTimeout        int // in millisecond
	TunaMaxPrice           string
	TunaNanoPayFee         string
	TunaMinNanoPayFee      string
	TunaNanoPayFeeRatio    float64
	TunaServiceName        string
	TunaSubscriptionPrefix string
	TunaIPFilter           *geo.IPFilter
	TunaNknFilter          *filter.NknFilter
	TunaDownloadGeoDB      bool
	TunaGeoDBPath          string
	TunaMeasureBandwidth   bool
	TunaMeasureStoragePath string
	SessionConfig          *ncp.Config
}

var defaultConfig = Config{
	NumTunaListeners:       4,
	TunaDialTimeout:        10000,
	TunaMaxPrice:           "0",
	TunaNanoPayFee:         "",
	TunaMinNanoPayFee:      "0",
	TunaNanoPayFeeRatio:    0.1,
	TunaServiceName:        tuna.DefaultReverseServiceName,
	TunaSubscriptionPrefix: tuna.DefaultSubscriptionPrefix,
	TunaIPFilter:           nil,
	TunaNknFilter:          nil,
	TunaDownloadGeoDB:      false,
	TunaGeoDBPath:          "",
	TunaMeasureBandwidth:   false,
	TunaMeasureStoragePath: "",
	SessionConfig:          nil,
}

func DefaultConfig() *Config {
	conf := defaultConfig
	conf.TunaIPFilter = &geo.IPFilter{}
	conf.TunaNknFilter = &filter.NknFilter{}
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
