package main

import (
	"fmt"

	"github.com/kartaladev/doger"
)

/**
 * Run example
 */

func main() {
	injector := doger.New()

	driverModule := injector.Scope("driver")
	passengerModule := injector.Scope("passenger")

	bootCarModule(injector)
	bootDriverModule(driverModule)
	bootPassengerModule(passengerModule)

	// 1 driver + 1 passenger
	driver := doger.MustInvoke[*Driver](driverModule)
	passenger := doger.MustInvokeNamed[*Passenger](passengerModule, "passenger-1")

	driver.TakeASeat()
	passenger.TakeASeat()

	fmt.Println(injector.ShutdownOnSignals())
}
