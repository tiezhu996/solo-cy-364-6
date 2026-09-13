package constants

import "testing"

func TestLossOrderStatusValid(t *testing.T) {
	cases := []struct {
		name string
		in   LossOrderStatus
		want bool
	}{
		{"pending", LossPending, true},
		{"approved", LossApproved, true},
		{"rejected", LossRejected, true},
		{"empty", "", false},
		{"unknown", "processed", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Valid(); got != tc.want {
				t.Fatalf("LossOrderStatus(%q).Valid() = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestCanLossTransition(t *testing.T) {
	cases := []struct {
		name string
		from LossOrderStatus
		to   LossOrderStatus
		want bool
	}{
		{"pending -> approved", LossPending, LossApproved, true},
		{"pending -> rejected", LossPending, LossRejected, true},
		{"approved -> rejected", LossApproved, LossRejected, false},
		{"rejected -> approved", LossRejected, LossApproved, false},
		{"approved -> approved", LossApproved, LossApproved, false},
		{"pending -> pending", LossPending, LossPending, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanLossTransition(tc.from, tc.to); got != tc.want {
				t.Fatalf("CanLossTransition(%q,%q) = %v, want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}
