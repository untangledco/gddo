.POSIX:
.SUFFIXES:

PREFIX=/usr/local
BINDIR=$(PREFIX)/bin

all: gddo

gddo:
	go build -o $@ ./cmd/$@

clean:
	rm -f gddo

install:
	mkdir -m755 -p $(DESTDIR)$(BINDIR)
	install -m755 gddo $(DESTDIR)$(BINDIR)

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/gddo

.PHONY: all gddo clean install uninstall
