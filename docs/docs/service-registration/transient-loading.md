---
title: Transient loading
description: Transient loading is a factory catalog
sidebar_position: 3
---

# Transient loading — a.k.a. Factory

Transient loading is similar to lazy loading, but a new instance is provided at every invocation. It can be considered as a factory registry.

## Provider {#provider}

A transient service must be defined with a `provider` (kind of constructor). This provider will be called at every invocation.

```go
type Provider[T any] func(doger.Injector) (T, error)
```

Example:

```go
type MyService struct {
    Hello string
}

func NewMyService(i doger.Injector) (*MyService, error) {
    return &MyService{Hello: "world"}, nil
}
```

## Inject service into DI container {#inject-service-into-di-container}

A service can be injected in many ways. Here is the full list of transient loading service injections.

```go
func ProvideTransient[T any](i doger.Injector, provider doger.Provider[T])
func ProvideNamedTransient[T any](i doger.Injector, name string, provider doger.Provider[T])
func OverrideTransient[T any](i doger.Injector, provider Provider[T])
func OverrideNamedTransient[T any](i doger.Injector, name string, provider doger.Provider[T])
```

:::info

It is highly recommended to use anonymous service invocation to let the framework handle naming.

:::

```go
i := doger.New()

doger.ProvideTransient(i, NewMyService)
// or
doger.ProvideNamedTransient(i, "my.really.cool.service", NewMyService)
```

**Play: https://go.dev/play/p/j69I52whJr2**

## Error handling {#error-handling}

On invocation, panics are caught by the framework and returned as an error.

## Hot service replacement {#hot-service-replacement}

By default, providing a service twice will panic. Service can be replaced at runtime using `doger.Override` helper.

For test purposes, it can be very useful to override a service given to the DI container.

:::warning

We strongly discourage using this helper in production. Please use service aliasing instead.

:::

```go
type CalculatorTestSuite struct {
    suite.Suite
    i doger.Injector
}

func (suite *CalculatorTestSuite) SetupSuite() {
    suite.i = doger.New()
    doger.ProvideTransient(suite.i, NewMyService)
}

func (suite *CalculatorTestSuite) TearDownSuite() {
    suite.i.Shutdown()
}

func (suite *CalculatorTestSuite) SetupTest() {
    doger.OverrideTransient(suite.i, NewMyServiceMock)  // <- replace service by mock
}

func (suite *CalculatorTestSuite) Test1() {
  // ...
}

func (suite *CalculatorTestSuite) Test2() {
  // ...
}
```
