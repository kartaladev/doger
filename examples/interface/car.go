package main

import "github.com/kartaladev/doger"

type Car interface {
	Start()
}

type carImplem struct {
	Engine Engine
	Wheels []*Wheel
}

func (c carImplem) Start() {
	println("vroooom")
}

func NewCar(i doger.Injector) (*carImplem, error) {
	wheels := []*Wheel{
		doger.MustInvokeNamed[*Wheel](i, "wheel-1"),
		doger.MustInvokeNamed[*Wheel](i, "wheel-2"),
		doger.MustInvokeNamed[*Wheel](i, "wheel-3"),
		doger.MustInvokeNamed[*Wheel](i, "wheel-4"),
	}

	engine := doger.MustInvoke[Engine](i)

	car := carImplem{
		Engine: engine,
		Wheels: wheels,
	}

	return &car, nil
}
