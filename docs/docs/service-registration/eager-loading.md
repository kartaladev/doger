---
title: Eager loading
description: Eager loading initializes a resource as soon as the code is executed.
sidebar_position: 2
---

# Eager loading

Eager loading is a service registration mode where the value is created immediately, before it's provided to the injector — unlike [lazy loading](./lazy-loading.md), there's no provider function and no deferred instantiation. It initializes a resource as soon as the code is executed.

Use eager loading for values that are already available at startup — configuration structs, parsed flags, or anything you construct yourself outside the DI container — and lazy loading for anything that needs a constructor with its own dependencies.

## Inject service into DI container {#inject-service-into-di-container}

An eager service is simply initialized by you and then injected into the DI container.

A service can be injected in many ways. Here is the full list of eager loading service injections.

```go
func ProvideValue[T any](i doger.Injector, value T)
func ProvideNamedValue[T any](i doger.Injector, name string, value T)
func OverrideValue[T any](i doger.Injector, value T)
func OverrideNamedValue[T any](i doger.Injector, name string, value T)
```

:::info

It is highly recommended to use anonymous service invocation to let the framework handle naming.

:::

```go
type Config struct {
  Port int
}
config := &Config{
  Port: os.Getenv("PORT"),
}

i := doger.New()

doger.ProvideValue(i, config)
// or
doger.ProvideNamedValue(i, "my.really.cool.config", config)
```

**Play: https://go.dev/play/p/5TOSiI-c17Y**

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
    doger.ProvideNamedValue(suite.i, "a-number", 42)
}

func (suite *CalculatorTestSuite) TearDownSuite() {
    suite.i.Shutdown()
}

func (suite *CalculatorTestSuite) SetupTest() {
    doger.OverrideNamedValue(suite.i, "a-number", 1337)  // <- replace service by mock
}

func (suite *CalculatorTestSuite) Test1() {
  // ...
}

func (suite *CalculatorTestSuite) Test2() {
  // ...
}
```

See also [lazy loading](./lazy-loading.md) for constructor-based registration, and [transient loading](./transient-loading.md) for a new instance on every invocation.
