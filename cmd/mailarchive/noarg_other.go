//go:build !windows && !darwin && !linux

package main

// readNoArgInputs has no OS inputs to read on other platforms, so defaultVerb's
// default arm gives today's usage refusal for a no-argument launch.
func readNoArgInputs(string) noArgInputs { return noArgInputs{} }
