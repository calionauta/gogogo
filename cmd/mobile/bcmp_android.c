// Android's libc does not export bcmp or __errno_location, but the Go
// runtime and some pure-Go shims reference them from the c-shared object.
// The _android suffix keeps this file out of every non-Android build.
#include <stddef.h>
#include <string.h>

int bcmp(const void *s1, const void *s2, size_t n) {
	return memcmp(s1, s2, n);
}

extern int *__errno(void);

int *__errno_location(void) {
	return __errno();
}

int __xpg_strerror_r(int errnum, char *buf, size_t buflen) {
	return strerror_r(errnum, buf, buflen);
}
