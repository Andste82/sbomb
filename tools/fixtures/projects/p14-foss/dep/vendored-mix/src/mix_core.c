/* SPDX-License-Identifier: MIT
 * Copyright (c) 2026 Fixture Vendored Mix Authors
 */
#include "vendored_mix.h"

int mix_core(int value)
{
    return mix_vendored_crc(value) + 1;
}
