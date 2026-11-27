PREFIX ?= /usr

all: tepache-hello

tepache-hello: hello.c
	$(CC) $(CPPFLAGS) $(CFLAGS) $(LDFLAGS) -o $@ hello.c -lcrypto

install: tepache-hello
	install -D -m 0755 tepache-hello $(DESTDIR)$(PREFIX)/bin/tepache-hello

clean:
	rm -f tepache-hello

.PHONY: all install clean
