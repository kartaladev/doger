package main

import (
	"github.com/kartaladev/doger"
)

/**
 * Driver
 */
type Driver struct {
	Seat   *Seat
	Engine *Engine
}

func (d *Driver) TakeASeat() {
	println("driver enters the car")
	d.Engine.Start()
}

func (d *Driver) Shutdown() {
	println("driver leaves the car")
}

func bootDriverModule(injector doger.Injector) {
	// provide driver
	doger.Provide(injector, func(i doger.Injector) (*Driver, error) {
		return &Driver{
			Seat:   doger.MustInvokeNamed[*Seat](i, "seat-1"),
			Engine: doger.MustInvoke[*Engine](i),
		}, nil
	})
}
