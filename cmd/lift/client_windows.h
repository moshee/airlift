#include "Windows.h"
#include "Wincrypt.h"
#include <stdio.h>
#include <string.h>

char* geterror(const char* sender);
char* CopyString(const char* str);
char* CryptPassword(const char* str, int inSize, void** out, int* outSize, int encrypt);
HANDLE GetTermInfo(CONSOLE_SCREEN_BUFFER_INFO* s);
void ClearLine(void);
void MoveUp(void);
