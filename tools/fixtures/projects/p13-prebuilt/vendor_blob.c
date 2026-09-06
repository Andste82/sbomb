/* A vendor's source. It is compiled at configure time, outside the build
 * graph, so that the resulting object is exactly what a prebuilt library
 * shipped by a third party looks like: no compile database entry, no build
 * edge, no depfile. Only its own debug information says where it came from. */
int vendor_answer(void)
{
	return 42;
}
