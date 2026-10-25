package main

import (
	"github.com/kartaladev/doger"
)

func main() {
	injector := doger.New()

	// provide wheels
	doger.ProvideNamedValue(injector, "wheel-1", NewWheel())
	doger.ProvideNamedValue(injector, "wheel-2", NewWheel())
	doger.ProvideNamedValue(injector, "wheel-3", NewWheel())
	doger.ProvideNamedValue(injector, "wheel-4", NewWheel())

	// provide car
	doger.Provide(injector, NewCar)

	// provide engine
	doger.Provide(injector, NewEngine)

	// magic happens here
	_ = doger.As[*carImplem, Car](injector)
	_ = doger.As[*engineImplem, Engine](injector)

	// start car
	car := doger.MustInvoke[Car](injector)
	car.Start()
}
