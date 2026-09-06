#include "mod_a.h"
#include "mod_b.h"

int main(void) { return (mod_a_value() + mod_b_value()) & 1; }
