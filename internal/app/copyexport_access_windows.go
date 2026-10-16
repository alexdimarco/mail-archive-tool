//go:build windows

package app

// destNotWritable: Windows folders have no mode bits to read without trying to write, so the
// preview says nothing and Start's refusal (R-w) is the answer.
func destNotWritable(dest string) string { return "" }
