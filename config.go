package main

import (
	"encoding/json"
	"flag"
	"os"
)

type Config struct {
	ServerPort int
	CabAddress string

	// TODO: Some config for the various existing web views, optionally point
	// to template like used for the scoreboard now.

	TextOutputPredictionModelName string
}

func DefaultConfig() *Config {
	return &Config{
		ServerPort:                    8080,
		CabAddress:                    "ws://kq.local:12749",
		TextOutputPredictionModelName: "",
	}
}

func ReadConfig(filepath string) (*Config, error) {
	config := DefaultConfig()
	f, err := os.Open(filepath)
	if err != nil {
		return config, err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	err = d.Decode(config)
	overrideByFlags(config)
	return config, err
}

var portFlag = flag.Int("port", 0, "the port number to listen on")
var modelFlag = flag.String("model", "", "the name of the model to use for predictions")

func overrideByFlags(config *Config) {
	if *portFlag > 0 {
		config.ServerPort = *portFlag
	}
	if len(*modelFlag) > 0 {
		config.TextOutputPredictionModelName = *modelFlag
	}
}
