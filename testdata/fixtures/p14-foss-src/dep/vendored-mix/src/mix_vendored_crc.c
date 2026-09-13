/* SPDX-License-Identifier: GPL-2.0-only
 * Copyright (c) 2026 Fixture Upstream CRC Authors
 *
 * Copied into this directory from another project, which is why it declares a
 * licence the directory's own LICENSE does not account for (section 22.5).
 */
#include "vendored_mix.h"

int mix_vendored_crc(int value)
{
    return (value * 7) & 0xff;
}
