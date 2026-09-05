int util_a(void);
int util_b(void);

int main(void) { return (util_a() + util_b()) & 1; }
