package service

// DenominationsSatang lists the nine tracked THB denominations, in satang,
// largest first. This is the canonical order used for storage, display, and the
// change-making DP.
var DenominationsSatang = []int64{100000, 50000, 10000, 5000, 2000, 1000, 500, 200, 100}

// TrackedDenom reports whether d (in satang) is one of the nine tracked
// denominations. Shared by the HTTP layers that validate client-supplied
// denomination keys, so the valid set lives in exactly one place.
func TrackedDenom(d int64) bool {
	for _, x := range DenominationsSatang {
		if x == d {
			return true
		}
	}
	return false
}

// denomsBaht is DenominationsSatang expressed in whole baht (since cash due is
// rounded to ฿1, every change amount is a whole-baht multiple). It is DERIVED
// from DenominationsSatang so the two can never drift out of sync — adding a
// denomination in one place automatically reflects in the other.
var denomsBaht = func() []int {
	d := make([]int, len(DenominationsSatang))
	for i, s := range DenominationsSatang {
		d[i] = int(s / 100)
	}
	return d
}()

// makeChangeBaht returns the minimum-count breakdown (denomBaht -> count) that
// forms changeBaht using only the supplied stock, and whether it is possible.
//
// It is a bounded coin-change DP: dp[v] is the fewest bills/coins to form v, and
// from[v] records the denomination taken last to reach v. Each denomination is
// processed once with a per-pass usage counter (num[]) so its stock limit is
// respected. Greedy-largest-first is deliberately NOT used: with limited stock it
// can wrongly report "impossible" when a valid combination exists, which would
// block a makeable sale.
func makeChangeBaht(changeBaht int, stock map[int]int) (map[int]int, bool) {
	if changeBaht == 0 {
		return map[int]int{}, true
	}
	// inf is a sentinel "unreachable" cost: large enough to exceed any real
	// coin count, yet small enough that inf+1 never overflows int.
	const inf = 1 << 30
	dp := make([]int, changeBaht+1)
	from := make([]int, changeBaht+1)
	for i := 1; i <= changeBaht; i++ {
		dp[i] = inf
		from[i] = -1
	}
	for _, d := range denomsBaht {
		c := stock[d]
		if c <= 0 || d > changeBaht {
			continue
		}
		num := make([]int, changeBaht+1) // count of d used to reach v in this pass
		for v := d; v <= changeBaht; v++ {
			if dp[v-d] == inf {
				continue
			}
			if dp[v-d]+1 < dp[v] && num[v-d]+1 <= c {
				dp[v] = dp[v-d] + 1
				num[v] = num[v-d] + 1
				from[v] = d
			}
		}
	}
	if dp[changeBaht] >= inf {
		return nil, false
	}
	out := make(map[int]int)
	for v := changeBaht; v > 0; {
		d := from[v]
		out[d]++
		v -= d
	}
	return out, true
}
