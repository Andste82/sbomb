#include "crypto.h"

int aes_round(int x) { return x ^ 0x5a; }
