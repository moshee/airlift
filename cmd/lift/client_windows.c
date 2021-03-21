#include "client_windows.h"

char* geterror(const char* sender) {
	DWORD code = GetLastError();
	char* msg;
	DWORD n = FormatMessage(
		FORMAT_MESSAGE_ALLOCATE_BUFFER|FORMAT_MESSAGE_FROM_SYSTEM|FORMAT_MESSAGE_IGNORE_INSERTS,
		NULL, code, 0, (LPSTR)&msg, 0, NULL);

	char* buf = (char*)malloc((size_t)n + strlen(sender) + 3);
	sprintf(buf, "%s: %s", sender, msg);
	return buf;
}

char* CopyString(const char* str) {
	if (!OpenClipboard(NULL)) {
		return geterror("CopyString");
	}

	// copy our UTF-8 text into a wchar string stored in the global handle
	size_t n = 0;
	mbstowcs_s(&n, NULL, 0, str, 0);

	// alloc and lock global memory
	HGLOBAL hMem = GlobalAlloc(GMEM_SHARE | GMEM_MOVEABLE, n*sizeof(wchar_t) + 1);
	LPTSTR glob = (LPTSTR)GlobalLock(hMem);

	mbstowcs_s(&n, glob, n + 1, str, n);

	GlobalUnlock(hMem);

	if (!SetClipboardData(CF_UNICODETEXT, hMem)) {
		return geterror("SetClipboardData");
	}

	if (!CloseClipboard()) {
		return geterror("CloseClipboard");
	}

	return NULL;
}

char* CryptPassword(const char* str, int inSize, void** out, int* outSize, int encrypt) {
	DATA_BLOB input, output;
	input.cbData = (DWORD)inSize + 1;
	input.pbData = (BYTE*)str;

	BOOL ok;
	if (encrypt) {
		ok = CryptProtectData(&input, NULL, NULL, NULL, NULL, 0, &output);
		if (!ok) {
			return geterror("CryptProtectData");
		}
	} else {
		ok = CryptUnprotectData(&input, NULL, NULL, NULL, NULL, 0, &output);
		if (!ok) {
			return geterror("CryptUnprotectData");
		}
	}

	*out = output.pbData;
	*outSize = output.cbData;

	return NULL;
}

HANDLE GetTermInfo(CONSOLE_SCREEN_BUFFER_INFO* s) {
	HANDLE hOut = GetStdHandle(STD_OUTPUT_HANDLE);
	GetConsoleScreenBufferInfo(hOut, s);
	return hOut;
}

void ClearLine(void) {
	CONSOLE_SCREEN_BUFFER_INFO s;
	HANDLE out = GetTermInfo(&s);

	COORD pos = { 0, s.dwCursorPosition.Y };
	DWORD n;

	FillConsoleOutputCharacter(out, ' ', s.dwSize.X, pos, (LPDWORD)(&n));
	SetConsoleCursorPosition(out, pos);
}

void MoveUp(void) {
	CONSOLE_SCREEN_BUFFER_INFO s;
	HANDLE out = GetTermInfo(&s);

	COORD pos = { 0, s.dwCursorPosition.Y - 2 };

	SetConsoleCursorPosition(out, pos);
}
