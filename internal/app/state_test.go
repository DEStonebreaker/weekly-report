package app

import "testing"

func TestDeliveryStateLocksAndRetainsUncertainAttempt(t *testing.T) {
	dir := t.TempDir()
	first, err := openDeliveryState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openDeliveryState(dir); err == nil {
		t.Error("overlapping process was not rejected")
	}
	key := "window/list/recipient"
	if err := first.set(key, DeliveryRecord{Status: "uncertain"}); err != nil {
		t.Fatal(err)
	}
	first.Close()
	second, err := openDeliveryState(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if record, ok := second.get(key); !ok || record.Status != "uncertain" {
		t.Errorf("record=%#v, %v", record, ok)
	}
}
