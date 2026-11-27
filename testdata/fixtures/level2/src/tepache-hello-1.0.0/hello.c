/* tepache-hello: paquete de prueba de nivel 2 (specs/008-source-builds, research D14).
 * Enlaza con libcrypto de OpenSSL: cada suite produce su propio binario. */
#include <stdio.h>
#include <string.h>
#include <openssl/crypto.h>

int main(int argc, char **argv)
{
	if (argc > 1 && strcmp(argv[1], "--version") == 0) {
		printf("tepache-hello 1.0.0\n");
		return 0;
	}
	printf("tepache-hello 1.0.0 (%s)\n", OpenSSL_version(OPENSSL_VERSION));
	return 0;
}
