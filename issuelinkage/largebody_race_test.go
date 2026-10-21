// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

//go:build race

package issuelinkage

import "time"

// largeBodyBound bounds TestExtract_LargeBodiesFinishInTime under the race
// detector, which slows the scan to about 13 s. It carries the same meaning
// as the bound without the detector: the line between a linear scan and a
// quadratic one, set far enough above the measurement that a slow runner
// alone cannot cross it.
const largeBodyBound = 180 * time.Second
