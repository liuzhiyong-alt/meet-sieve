package malgo

/*
#include <stdlib.h>
*/
import "C"

import "unsafe"

// freeDeviceIDPointer 释放 malgo DeviceID.Pointer 使用 C.CBytes 分配的设备标识。
func freeDeviceIDPointer(pointer unsafe.Pointer) {
	if pointer != nil {
		C.free(pointer)
	}
}
