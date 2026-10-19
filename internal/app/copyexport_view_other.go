//go:build !linux && !darwin && !windows

package app

// classifyView on another unix does not know how a path is reached, so it says
// stacking: identity is never trusted to decide there (design §4.7: a view the
// classifier does not know is treated as stacking).
func classifyView(abs string) ceView { return ceView{Kind: viewStacking} }
