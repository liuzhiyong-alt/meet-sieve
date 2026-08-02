//go:build darwin

package singleinstance

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework Foundation
#import <AppKit/AppKit.h>

static int meetSieveActivateRunningApplication(void) {
	@autoreleasepool {
		NSArray<NSRunningApplication *> *applications =
			[NSRunningApplication runningApplicationsWithBundleIdentifier:@"com.meetsieve.app"];
		pid_t currentProcessID = [[NSProcessInfo processInfo] processIdentifier];
		for (NSRunningApplication *application in applications) {
			if ([application processIdentifier] == currentProcessID || [application isTerminated]) {
				continue;
			}
			// 由用户发起的第二次启动触发激活，不强制抢占其他应用焦点。
			[application activateWithOptions:0];
			return 1;
		}
	}
	return 0;
}
*/
import "C"

// Acquire 通过 LaunchServices 和 AppKit 确保当前 macOS 登录会话只启动一个 MeetSieve 应用。
func Acquire() (Outcome, *Lease, error) {
	if C.meetSieveActivateRunningApplication() != 0 {
		return OutcomeAlreadyRunning, nil, nil
	}
	return OutcomeAcquired, newLease(nil), nil
}
