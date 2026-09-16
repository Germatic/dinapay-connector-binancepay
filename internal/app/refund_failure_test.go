package app

import (
	"testing"

	"github.com/Germatic/dinapay-connector-binancepay/internal/core"
	contract "github.com/Germatic/dinapay-contracts/go/connectorcontract/failures"
)

func TestNormalizeRefundFailureSeparatesPublicAndNativeData(t *testing.T) {
	refund := core.ProviderRefund{Status: "failed", RawStatus: "REFUND_FAIL"}
	normalizeRefundFailure(&refund)
	if refund.Failure == nil || refund.Failure.Code != string(contract.RefundRejected) || !contract.ValidRefund(*refund.Failure) {
		t.Fatalf("failure=%#v", refund.Failure)
	}
	if refund.ProviderFailure == nil || refund.ProviderFailure.Code != "REFUND_FAIL" {
		t.Fatalf("providerFailure=%#v", refund.ProviderFailure)
	}
}

func TestNormalizeRefundFailureLeavesPendingClean(t *testing.T) {
	refund := core.ProviderRefund{Status: "pending", RawStatus: "PROCESSING"}
	normalizeRefundFailure(&refund)
	if refund.Failure != nil || refund.ProviderFailure != nil {
		t.Fatalf("unexpected failure: %#v %#v", refund.Failure, refund.ProviderFailure)
	}
}
