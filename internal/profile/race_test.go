// SPDX-License-Identifier: Apache-2.0

//go:build race

package profile

// raceEnabled skips the tests that encode a whole animation: under the race
// detector one takes minutes.
const raceEnabled = true
