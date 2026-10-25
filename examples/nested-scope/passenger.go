package main

import (
	"github.com/kartaladev/doger"
)

/**
 * Passenger
 */
type Passenger struct {
	Seat *Seat
}

func (p *Passenger) TakeASeat() {
	println("passenger enters the car")
	p.Seat.Warm()
}

func (p *Passenger) Shutdown() {
	println("passenger leaves the car")
}

/**
 * Run example
 */
func bootPassengerModule(injector doger.Injector) {
	// provide passenger
	doger.ProvideNamed(injector, "passenger-1", func(i doger.Injector) (*Passenger, error) {
		return &Passenger{
			Seat: doger.MustInvokeNamed[*Seat](i, "seat-2"),
		}, nil
	})
	doger.ProvideNamed(injector, "passenger-2", func(i doger.Injector) (*Passenger, error) {
		return &Passenger{
			Seat: doger.MustInvokeNamed[*Seat](i, "seat-3"),
		}, nil
	})
	doger.ProvideNamed(injector, "passenger-3", func(i doger.Injector) (*Passenger, error) {
		return &Passenger{
			Seat: doger.MustInvokeNamed[*Seat](i, "seat-4"),
		}, nil
	})
}
