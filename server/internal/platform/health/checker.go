package health

import (
	"context"
	"time"
)

const (
	StatusUp   = "up"
	StatusDown = "down"
)

type Dependency interface {
	Name() string
	Ping(context.Context) error
}

type Result struct {
	Ready  bool              `json:"-"`
	Checks map[string]string `json:"checks"`
}

type Checker struct {
	timeout      time.Duration
	dependencies []Dependency
}

func NewChecker(timeout time.Duration, dependencies ...Dependency) *Checker {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &Checker{
		timeout:      timeout,
		dependencies: append([]Dependency(nil), dependencies...),
	}
}

func (c *Checker) Check(ctx context.Context) Result {
	result := Result{Ready: true, Checks: make(map[string]string, len(c.dependencies))}
	if len(c.dependencies) == 0 {
		return result
	}

	checkCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	type dependencyResult struct {
		name string
		err  error
	}
	results := make(chan dependencyResult, len(c.dependencies))
	for _, dependency := range c.dependencies {
		dependency := dependency
		go func() {
			results <- dependencyResult{name: dependency.Name(), err: dependency.Ping(checkCtx)}
		}()
	}

	remaining := make(map[string]struct{}, len(c.dependencies))
	for _, dependency := range c.dependencies {
		remaining[dependency.Name()] = struct{}{}
	}
	for range c.dependencies {
		select {
		case dependency := <-results:
			delete(remaining, dependency.name)
			if dependency.err != nil {
				result.Ready = false
				result.Checks[dependency.name] = StatusDown
				continue
			}
			result.Checks[dependency.name] = StatusUp
		case <-checkCtx.Done():
			result.Ready = false
			for name := range remaining {
				result.Checks[name] = StatusDown
			}
			return result
		}
	}
	return result
}
