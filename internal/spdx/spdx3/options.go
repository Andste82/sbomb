package spdx3

// CustomAdditions is true: SPDX 3.0.1 names an exception the SPDX list does
// not carry as an AdditionRef (mapping.Options.CustomAdditions). The
// spdx-json writer's version table takes it from here, and so do this
// package's tests.
const CustomAdditions = true
