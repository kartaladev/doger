package fixtures

import "github.com/kartaladev/doger"

/**
 * driver
 */
func newDriver(i doger.Injector) (*Driver, error) {
	d := &Driver{
		seat:   doger.MustInvokeNamed[*Seat](i, "seat-1"),
		engine: doger.MustInvoke[*Engine](i),
	}

	d.seat.take()
	d.engine.start()

	return d, nil
}

type Driver struct {
	seat   *Seat
	engine *Engine
}

func (d *Driver) Shutdown() {
	d.engine.stop()
	d.seat.release()
}

/**
 * passenger
 */
func newPassenger(seatName string) func(i doger.Injector) (*Passenger, error) {
	return func(i doger.Injector) (*Passenger, error) {
		p := &Passenger{
			seat: doger.MustInvokeNamed[*Seat](i, seatName),
		}

		p.seat.take()

		return p, nil
	}
}

type Passenger struct {
	seat *Seat
}

func (p *Passenger) Shutdown() {
	p.seat.release()
}

/**
 * Seat
 */
type Seat struct {
	busy bool
}

func (s *Seat) take() {
	if s.busy {
		panic("seat should be free")
	}
	s.busy = true
}

func (s *Seat) release() {
	if !s.busy {
		panic("seat should be busy")
	}
	s.busy = false
}

func (s *Seat) Shutdown() {
	if s.busy {
		panic("seat should be free")
	}
}

/**
 * Wheel
 */
type Wheel struct {
	active bool
}

func (w *Wheel) start() {
	if w.active {
		panic("wheel should be stopped")
	}
	w.active = true
}

func (w *Wheel) stop() {
	if !w.active {
		panic("wheel should be started")
	}
	w.active = false
}

func (w *Wheel) Shutdown() {
	if w.active {
		panic("wheel should be stopped")
	}
}

/**
 * engine
 */
func newEngine(i doger.Injector) (*Engine, error) {
	return &Engine{
		wheels: []*Wheel{
			doger.MustInvokeNamed[*Wheel](i, "wheel-1"),
			doger.MustInvokeNamed[*Wheel](i, "wheel-2"),
			doger.MustInvokeNamed[*Wheel](i, "wheel-3"),
			doger.MustInvokeNamed[*Wheel](i, "wheel-4"),
		},
	}, nil
}

type Engine struct {
	started bool
	wheels  []*Wheel
}

func (e *Engine) start() {
	if e.started {
		panic("engine should be stopped")
	}
	e.started = true

	for _, wheel := range e.wheels {
		wheel.start()
	}
}

func (e *Engine) stop() {
	if !e.started {
		panic("engine should be started")
	}
	e.started = false

	for _, wheel := range e.wheels {
		wheel.stop()
	}
}

func (e *Engine) Shutdown() {
	if e.started {
		panic("engine should be stopped")
	}
}

func GetPackage() (doger.Injector, doger.Injector, doger.Injector) {
	injector := doger.New()

	driverScope := injector.Scope("driver")
	passengerScope := injector.Scope("passenger")

	// provide wheels
	doger.ProvideNamedValue(injector, "wheel-1", &Wheel{})
	doger.ProvideNamedValue(injector, "wheel-2", &Wheel{})
	doger.ProvideNamedValue(injector, "wheel-3", &Wheel{})
	doger.ProvideNamedValue(injector, "wheel-4", &Wheel{})

	// provide engine
	doger.Provide(injector, newEngine)

	// provide seats
	doger.ProvideNamedValue(injector, "seat-1", &Seat{})
	doger.ProvideNamedValue(injector, "seat-2", &Seat{})
	doger.ProvideNamedValue(injector, "seat-3", &Seat{})
	doger.ProvideNamedValue(injector, "seat-4", &Seat{})

	// provide driver
	doger.Provide(driverScope, newDriver)

	// provide passenger
	doger.ProvideNamed(passengerScope, "passenger-1", newPassenger("seat-2"))
	doger.ProvideNamed(passengerScope, "passenger-2", newPassenger("seat-3"))
	doger.ProvideNamed(passengerScope, "passenger-3", newPassenger("seat-4"))

	return injector, driverScope, passengerScope
}
