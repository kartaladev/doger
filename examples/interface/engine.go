package main

import "github.com/kartaladev/doger"

type Engine interface{}

type engineImplem struct{}

func NewEngine(i doger.Injector) (*engineImplem, error) {
	return &engineImplem{}, nil
}
