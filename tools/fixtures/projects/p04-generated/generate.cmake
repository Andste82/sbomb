file(READ "${INPUT}" value)
string(STRIP "${value}" value)
file(WRITE "${OUTPUT}" "int generated_version(void) { return ${value}; }\n")
