package iso18626

import "testing"

func TestIsTransitionValid(t *testing.T) {
	tests := []struct {
		from, to TypeStatus
		expected bool
	}{
		{"whatever", "", false},                // Empty to should not allow transition
		{"", "", false},                        // Empty to should not allow transition
		{TypeStatusRequestReceived, "", false}, // Empty to should not allow transition
		{"", "whatever", false},                // Empty from should allow transition to valid status only
		{"", TypeStatusRequestReceived, true},  // Empty from should allow transition to valid status only
		{"", TypeStatusRetryPossible, true},    // Empty from should allow transition to valid status only
		{"", TypeStatusExpectToSupply, true},   // Empty from should allow transition to valid status only
		{"", TypeStatusWillSupply, true},       // Empty from should allow transition to valid status only
		{"", TypeStatusLoaned, true},           // Empty from should allow transition to valid status only
		{"", TypeStatusOverdue, true},          // Empty from should allow transition to valid status only
		{"", TypeStatusRecalled, true},         // Empty from should allow transition to valid status only
		{"", TypeStatusRetryPossible, true},    // Empty from should allow transition to valid status only
		{"", TypeStatusUnfilled, true},         // Empty from should allow transition to valid status only
		{"", TypeStatusCopyCompleted, true},    // Empty from should allow transition to valid status only
		{"", TypeStatusLoanCompleted, true},    // Empty from should allow transition to valid status only
		{TypeStatusRequestReceived, TypeStatusRequestReceived, false},
		{TypeStatusRequestReceived, "", false},
		{TypeStatusRequestReceived, TypeStatusRequestReceived, false},
		{TypeStatusRequestReceived, TypeStatusRetryPossible, true},
		{TypeStatusRequestReceived, TypeStatusExpectToSupply, true},
		{TypeStatusRequestReceived, TypeStatusWillSupply, true},
		{TypeStatusRequestReceived, TypeStatusLoaned, true},
		{TypeStatusRequestReceived, TypeStatusUnfilled, true},
		{TypeStatusRequestReceived, TypeStatusLoaned, true},
		{TypeStatusRequestReceived, TypeStatusLoanCompleted, true},
		{TypeStatusExpectToSupply, TypeStatusRequestReceived, false},
		{TypeStatusExpectToSupply, TypeStatusRetryPossible, false},
		{TypeStatusExpectToSupply, TypeStatusExpectToSupply, false},
		{TypeStatusExpectToSupply, TypeStatusWillSupply, true},
		{TypeStatusExpectToSupply, TypeStatusLoaned, true},
		{TypeStatusExpectToSupply, TypeStatusUnfilled, true},
		{TypeStatusExpectToSupply, TypeStatusLoanCompleted, true},
		{TypeStatusWillSupply, TypeStatusRequestReceived, false},
		{TypeStatusWillSupply, TypeStatusRetryPossible, false},
		{TypeStatusWillSupply, TypeStatusExpectToSupply, false},
		{TypeStatusWillSupply, TypeStatusWillSupply, false},
		{TypeStatusWillSupply, TypeStatusLoaned, true},
		{TypeStatusWillSupply, TypeStatusUnfilled, true},
		{TypeStatusWillSupply, TypeStatusLoanCompleted, true},
		{TypeStatusLoaned, TypeStatusRequestReceived, false},
		{TypeStatusLoaned, TypeStatusExpectToSupply, false},
		{TypeStatusLoaned, TypeStatusWillSupply, false},
		{TypeStatusLoaned, TypeStatusOverdue, true},
		{TypeStatusLoaned, TypeStatusRecalled, true},
		{TypeStatusLoaned, TypeStatusCancelled, true},
		{TypeStatusLoaned, TypeStatusCompletedWithoutReturn, true},
		{TypeStatusLoaned, TypeStatusCopyCompleted, true},
		{TypeStatusOverdue, TypeStatusRequestReceived, false},
		{TypeStatusOverdue, TypeStatusExpectToSupply, false},
		{TypeStatusOverdue, TypeStatusWillSupply, false},
		{TypeStatusOverdue, TypeStatusLoaned, true},
		{TypeStatusOverdue, TypeStatusRecalled, true},
		{TypeStatusOverdue, TypeStatusCancelled, true},
		{TypeStatusOverdue, TypeStatusCompletedWithoutReturn, true},
		{TypeStatusOverdue, TypeStatusCopyCompleted, true},
		{TypeStatusRecalled, TypeStatusRequestReceived, false},
		{TypeStatusRecalled, TypeStatusExpectToSupply, false},
		{TypeStatusRecalled, TypeStatusWillSupply, false},
		{TypeStatusRecalled, TypeStatusLoaned, false},
		{TypeStatusRecalled, TypeStatusOverdue, false},
		{TypeStatusRecalled, TypeStatusCancelled, true},
		{TypeStatusRecalled, TypeStatusCompletedWithoutReturn, true},
		{TypeStatusRecalled, TypeStatusCopyCompleted, true},
		{TypeStatusRetryPossible, TypeStatusRequestReceived, false},
		{TypeStatusRetryPossible, TypeStatusExpectToSupply, true},
		{TypeStatusRetryPossible, TypeStatusWillSupply, true},
		{TypeStatusRetryPossible, TypeStatusLoaned, true},
		{TypeStatusRetryPossible, TypeStatusOverdue, true},
		{TypeStatusRetryPossible, TypeStatusRecalled, true},
		{TypeStatusRetryPossible, TypeStatusCancelled, true},
		{TypeStatusRetryPossible, TypeStatusCompletedWithoutReturn, true},
		{TypeStatusRetryPossible, TypeStatusCopyCompleted, true},
		{TypeStatusUnfilled, TypeStatusRequestReceived, false},
		{TypeStatusUnfilled, TypeStatusExpectToSupply, false},
		{TypeStatusUnfilled, TypeStatusWillSupply, false},
		{TypeStatusUnfilled, TypeStatusLoaned, false},
		{TypeStatusUnfilled, TypeStatusOverdue, false},
		{TypeStatusUnfilled, TypeStatusRecalled, false},
		{TypeStatusUnfilled, TypeStatusCancelled, false},
		{TypeStatusUnfilled, TypeStatusCompletedWithoutReturn, false},
		{TypeStatusUnfilled, TypeStatusCopyCompleted, false},
		{TypeStatusCopyCompleted, TypeStatusRequestReceived, false},
		{TypeStatusCopyCompleted, TypeStatusExpectToSupply, false},
		{TypeStatusCopyCompleted, TypeStatusWillSupply, false},
		{TypeStatusCopyCompleted, TypeStatusLoaned, false},
		{TypeStatusCopyCompleted, TypeStatusOverdue, false},
		{TypeStatusCopyCompleted, TypeStatusRecalled, false},
		{TypeStatusCopyCompleted, TypeStatusCancelled, false},
		{TypeStatusCopyCompleted, TypeStatusCompletedWithoutReturn, false},
		{TypeStatusCopyCompleted, TypeStatusLoanCompleted, false},
		{TypeStatusLoanCompleted, TypeStatusRequestReceived, false},
		{TypeStatusLoanCompleted, TypeStatusExpectToSupply, false},
		{TypeStatusLoanCompleted, TypeStatusWillSupply, false},
		{TypeStatusLoanCompleted, TypeStatusLoaned, false},
		{TypeStatusLoanCompleted, TypeStatusOverdue, false},
		{TypeStatusLoanCompleted, TypeStatusRecalled, false},
		{TypeStatusLoanCompleted, TypeStatusCancelled, false},
		{TypeStatusLoanCompleted, TypeStatusCompletedWithoutReturn, false},
		{TypeStatusLoanCompleted, TypeStatusCopyCompleted, false},
		{TypeStatusLoanCompleted, TypeStatusLoanCompleted, false},
		{TypeStatusCompletedWithoutReturn, TypeStatusRequestReceived, false},
		{TypeStatusCompletedWithoutReturn, TypeStatusExpectToSupply, false},
		{TypeStatusCompletedWithoutReturn, TypeStatusWillSupply, false},
		{TypeStatusCompletedWithoutReturn, TypeStatusLoaned, false},
		{TypeStatusCompletedWithoutReturn, TypeStatusOverdue, false},
		{TypeStatusCompletedWithoutReturn, TypeStatusRecalled, false},
		{TypeStatusCompletedWithoutReturn, TypeStatusCancelled, false},
		{TypeStatusCompletedWithoutReturn, TypeStatusCopyCompleted, false},
		{TypeStatusCompletedWithoutReturn, TypeStatusLoanCompleted, false},
		{TypeStatusCancelled, TypeStatusRequestReceived, false},
		{TypeStatusCancelled, TypeStatusExpectToSupply, false},
		{TypeStatusCancelled, TypeStatusWillSupply, false},
		{TypeStatusCancelled, TypeStatusLoaned, false},
		{TypeStatusCancelled, TypeStatusOverdue, false},
		{TypeStatusCancelled, TypeStatusRecalled, false},
		{TypeStatusCancelled, TypeStatusCompletedWithoutReturn, false},
		{TypeStatusCancelled, TypeStatusCopyCompleted, false},
		{TypeStatusCancelled, TypeStatusCompletedWithoutReturn, false},
		{TypeStatusCancelled, TypeStatusLoanCompleted, false},
		{TypeStatusCompletedWithoutReturn, TypeStatusLoaned, false},
		{TypeStatusCompletedWithoutReturn, TypeStatusOverdue, false},
		{TypeStatusCompletedWithoutReturn, TypeStatusRecalled, false},
		{TypeStatusCompletedWithoutReturn, TypeStatusCancelled, false},
		{TypeStatusCompletedWithoutReturn, TypeStatusCompletedWithoutReturn, false},
		{TypeStatusCompletedWithoutReturn, TypeStatusCopyCompleted, false},
		{TypeStatusCompletedWithoutReturn, TypeStatusLoanCompleted, false},
	}

	for _, test := range tests {
		result := IsTransitionValid(test.from, test.to)
		if result != test.expected {
			t.Errorf("IsTransitionValid(%v, %v) = %v; want %v", test.from, test.to, result, test.expected)
		}
	}
}
