/* SPDX-License-Identifier: BSD-3-Clause
 * Copyright (c) 2026, Fixture BSD Header Authors
 *
 * Header-only: there is no build file and no source file in this component,
 * so a LICENSE beside an include/ directory is the only thing that marks its
 * boundary.
 */
#ifndef BSD_HDR_H
#define BSD_HDR_H

static inline int bsd_scale(int value)
{
    return value << 1;
}

#endif
