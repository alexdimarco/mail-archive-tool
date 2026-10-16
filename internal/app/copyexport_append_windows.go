//go:build windows

package app

// appendFlag is nothing on Windows: Go opens an O_APPEND file with FILE_APPEND_DATA and
// without FILE_WRITE_DATA, and FlushFileBuffers wants write access, which every append of
// the run record and the journal makes (it fsyncs each line). A write there seeks to the
// end under recordWriteMu instead.
const appendFlag = 0
