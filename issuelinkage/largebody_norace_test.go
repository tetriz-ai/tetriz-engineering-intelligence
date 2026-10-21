// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

//go:build !race

package issuelinkage

import "time"

// largeBodyBound bounds TestExtract_LargeBodiesFinishInTime. The scan takes
// about 0.6 s, so this is not a performance budget — it is the line between
// a linear scan and a quadratic one, which takes minutes on these inputs.
// Set it far above the measurement: a bound a small multiple away turns a
// loaded or shared runner into a red build without a regression behind it.
const largeBodyBound = 30 * time.Second
