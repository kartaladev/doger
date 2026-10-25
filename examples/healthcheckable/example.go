package main

import (
	"context"
	"fmt"
	"time"

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

func (a *AutoPilot) HealthCheckWithContext(ctx context.Context) error {
	return nil
}

/**
 * Engine
 */
type Engine struct{}

func (e *Engine) HealthCheck() error {
	return fmt.Errorf("engine broken")
}

/**
 * Car
 */
type Car struct {
	AutoPilot *AutoPilot
	Engine    *Engine
	Wheels    []*Wheel
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

	// check single service
	fmt.Println(doger.HealthCheck[*Engine](injector))

	// check all services
	fmt.Println(injector.HealthCheck())
	// or
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	fmt.Println(injector.HealthCheckWithContext(ctx))
}
