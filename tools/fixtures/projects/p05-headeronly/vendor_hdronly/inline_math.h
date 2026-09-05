/* SPDX-License-Identifier: MIT */
#pragma once

static inline int clamp_low(int value) { return value < 0 ? 0 : value; }
