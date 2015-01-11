package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Security -framework Foundation -framework Cocoa
#import <Foundation/Foundation.h>
#import <Security/Security.h>
#import <Cocoa/Cocoa.h>

char* errorString(OSStatus status) {
	CFStringRef str = SecCopyErrorMessageString(status, NULL);
	if (str == NULL) return NULL;

	CFIndex length = CFStringGetLength(str);
	CFIndex maxSize = CFStringGetMaximumSizeForEncoding(length, kCFStringEncodingUTF8);
	char* buffer = (char*)malloc(maxSize);

	if (CFStringGetCString(str, buffer, maxSize, kCFStringEncodingUTF8)) {
		return buffer;
	}

	return NULL;
}

const char* copyString(const char* data) {
	NSString* str = [NSString stringWithCString:data encoding:NSUTF8StringEncoding];

	NSPasteboard* pboard = [NSPasteboard generalPasteboard];

	if (pboard == nil) {
		return "(+generalPasteboard) failed to get general pasteboard - are you on tmux?";
	}

	[pboard declareTypes:[NSArray arrayWithObject:NSPasteboardTypeString] owner:nil];
	[pboard clearContents];

	const char* msg = NULL;

	@try {
		if (![pboard writeObjects:[NSArray arrayWithObject:str]]) {
			msg = "(-writeObjects:) pasteboard ownership changed";
		}
	}
	@catch (NSException* e) {
		NSString* err = [NSString stringWithFormat:@"%@: %@", e.name, e.reason];
		msg = [err cStringUsingEncoding:NSUTF8StringEncoding];
	}
	return msg;
}
*/
import "C"
import (
	"errors"
	"unsafe"
)

type Config struct {
	Scheme string
	Host   string
	Port   string
}

const (
	serviceName              = "airlift"
	PasswordStorageMechanism = "the OSX Keychain"
)

func addPassword(conf *Config, password string) error {
	status := C.SecKeychainAddGenericPassword(nil,
		C.UInt32(len(serviceName)), C.CString(serviceName),
		C.UInt32(len(conf.Host)), C.CString(conf.Host),
		C.UInt32(len(password)), unsafe.Pointer(C.CString(password)),
		nil)

	if status != 0 {
		err := C.GoString(C.errorString(status))
		return errors.New(err)
	}
	return nil
}

func getPassword(conf *Config) (string, error) {
	var (
		size C.UInt32
		data unsafe.Pointer
	)

	status := C.SecKeychainFindGenericPassword(nil,
		C.UInt32(len(serviceName)), C.CString(serviceName),
		C.UInt32(len(conf.Host)), C.CString(conf.Host),
		&size, &data,
		nil)

	switch status {
	case 0:
		return string(C.GoBytes(data, C.int(size))), nil
	case C.errSecItemNotFound:
		return "", errPassNotFound
	default:
		return "", errors.New(C.GoString(C.errorString(status)))
	}
}

func deletePassword(conf *Config) error {
	var (
		item C.SecKeychainItemRef
	)

	status := C.SecKeychainFindGenericPassword(nil,
		C.UInt32(len(serviceName)), C.CString(serviceName),
		C.UInt32(len(conf.Host)), C.CString(conf.Host),
		nil, nil,
		&item)

	if status != 0 {
		err := C.GoString(C.errorString(status))
		return errors.New(err)
	}

	status = C.SecKeychainItemDelete(item)

	if status != 0 {
		err := C.GoString(C.errorString(status))
		return errors.New(err)
	}

	return nil
}

// update or add
func updatePassword(conf *Config, newpass string) error {
	var (
		item C.SecKeychainItemRef
	)

	status := C.SecKeychainFindGenericPassword(nil,
		C.UInt32(len(serviceName)), C.CString(serviceName),
		C.UInt32(len(conf.Host)), C.CString(conf.Host),
		nil, nil,
		&item)

	switch status {
	case 0:
		break
	case C.errSecItemNotFound:
		return addPassword(conf, newpass)
	default:
		return errors.New(C.GoString(C.errorString(status)))
	}

	status = C.SecKeychainItemModifyContent(item, nil,
		C.UInt32(len(newpass)), unsafe.Pointer(C.CString(newpass)))

	if status != 0 {
		return errors.New(C.GoString(C.errorString(status)))
	}

	return nil
}

func copyString(s string) error {
	msg := C.GoString(C.copyString(C.CString(s)))
	if len(msg) == 0 {
		return nil
	}

	return errors.New("Pasteboard error: " + msg)
}
