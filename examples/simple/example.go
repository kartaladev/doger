package main

import (
	"github.com/kartaladev/doger"
)

/**
 * Wheel
 */
type Wheel struct{}

/**
 * Engine
 */
type Engine struct{}

/**
 * Car
 */
type Car struct {
	Engine *Engine
	Wheels []*Wheel
}

func (c *Car) Start() {
	println("vroooom")
}

/**
 * Run example
 */
func main() {
	injector := doger.New()

	// provide wheels
	doger.ProvideNamedValue(injector, "wheel-1", &Wheel{})
	doger.ProvideNamedValue(injector, "wheel-2", &Wheel{})
	doger.ProvideNamedValue(injector, "wheel-3", &Wheel{})
	doger.ProvideNamedValue(injector, "wheel-4", &Wheel{})

	// provide car
	doger.Provide(injector, func(i doger.Injector) (*Car, error) {
		car := Car{
			Engine: doger.MustInvoke[*Engine](i),
			Wheels: []*Wheel{
				doger.MustInvokeNamed[*Wheel](i, "wheel-1"),
				doger.MustInvokeNamed[*Wheel](i, "wheel-2"),
				doger.MustInvokeNamed[*Wheel](i, "wheel-3"),
				doger.MustInvokeNamed[*Wheel](i, "wheel-4"),
			},
		}

		return &car, nil
	})

	// provide engine
	doger.Provide(injector, func(i doger.Injector) (*Engine, error) {
		return &Engine{}, nil
	})

	// start car
	car := doger.MustInvoke[*Car](injector)
	car.Start()
}
