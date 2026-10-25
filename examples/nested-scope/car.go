package main

import (
	"github.com/kartaladev/doger"
)

/**
 * Seat
 */
type Seat struct{}

func (s *Seat) Warm() {
	println("🔥🔥🔥")
}

func (s *Seat) Shutdown() {
	println("stopping seat")
}

/**
 * Wheel
 */
type Wheel struct{}

func (w *Wheel) Shutdown() {
	println("stopping wheel")
}

/**
 * Engine
 */
type Engine struct {
	Wheels []*Wheel
}

func (e *Engine) Start() {
	println("vroooom")
}

func (e *Engine) Shutdown() {
	println("stopping engine")
}

func bootCarModule(injector doger.Injector) {
	// provide wheels
	doger.ProvideNamedValue(injector, "wheel-1", &Wheel{})
	doger.ProvideNamedValue(injector, "wheel-2", &Wheel{})
	doger.ProvideNamedValue(injector, "wheel-3", &Wheel{})
	doger.ProvideNamedValue(injector, "wheel-4", &Wheel{})

	// provide engine
	doger.Provide(injector, func(i doger.Injector) (*Engine, error) {
		return &Engine{
			Wheels: []*Wheel{
				doger.MustInvokeNamed[*Wheel](i, "wheel-1"),
				doger.MustInvokeNamed[*Wheel](i, "wheel-2"),
				doger.MustInvokeNamed[*Wheel](i, "wheel-3"),
				doger.MustInvokeNamed[*Wheel](i, "wheel-4"),
			},
		}, nil
	})

	// provide seats
	doger.ProvideNamedValue(injector, "seat-1", &Seat{})
	doger.ProvideNamedValue(injector, "seat-2", &Seat{})
	doger.ProvideNamedValue(injector, "seat-3", &Seat{})
	doger.ProvideNamedValue(injector, "seat-4", &Seat{})
}
