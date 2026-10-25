package main

import (
	"context"
	"fmt"

	"github.com/kartaladev/doger"
)

/**
 * Wheel
 */
type Wheel struct{}

/**
 * AutoPilot
 */
type AutoPilot struct{}

func (a *AutoPilot) ShutdownWithContext(ctx context.Context) error {
	return nil
}

/**
 * Engine
 */
type Engine struct{}

func (c *Engine) Shutdown() error {
	println("engine stopped")
	return nil
}

/**
 * Car
 */
type Car struct {
	AutoPilot *AutoPilot
	Engine    *Engine
	Wheels    []*Wheel
}

func (c *Car) Shutdown() error {
	println("car stopped")
	return fmt.Errorf("💥 BOOOOM!") //nolint:staticcheck
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
			AutoPilot: doger.MustInvoke[*AutoPilot](i),
			Engine:    doger.MustInvoke[*Engine](i),
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

	// provide autopilot
	doger.Provide(injector, func(i doger.Injector) (*AutoPilot, error) {
		return &AutoPilot{}, nil
	})

	// start car
	car := doger.MustInvoke[*Car](injector)
	car.Start()

	_, err := injector.ShutdownOnSignals()
	if err != nil {
		fmt.Println(err.Error())
	}
}
