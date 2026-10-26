package presync

// StoreRecordForTest writes an empty pre-sync state record at path. It exists for
// the settings-guard test in package presync_test, which can import encmark (the
// guard's installer) where this package cannot.
func StoreRecordForTest(path string) error { return storeRecord(path, record{}) }
