/* SPDX-License-Identifier: MIT
 * Copyright (c) 2026 sbomb fixture authors
 *
 * The application of the FOSS attribution fixture. Every dependency it names
 * here is linked into the deliverable; the GPL-2.0 generator that produced
 * table.c is not.
 */
#include <stdio.h>

#include "apache_lib.h"
#include "bsd_hdr.h"
#include "lgpl_lib.h"
#include "mit_lib.h"
#include "multi.h"
#include "nocopyright.h"
#include "nolicense.h"

extern const int foss_table[8];

int main(void)
{
    int total = foss_table[3];

    total += mit_used_a(1);
    total += apache_one(2);
    total += apache_two(3);
    total += lgpl_core(4);
    total += lgpl_extra(5);
    total += multi_pick(6);
    total += nolicense_sum(7);
    total += nocopyright_mix(8);
    total += bsd_scale(9);

    printf("p14-foss total %d\n", total);
    return 0;
}
