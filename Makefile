.POSIX:
.SUFFIXES:

PREFIX=/usr/local
BINDIR=$(PREFIX)/bin
SHAREDIR=$(PREFIX)/share/gddo

all: gddo

gddo:
	go build \
		-ldflags "-X main.ShareDir=$(SHAREDIR)" \
		-o $@ \
		./cmd/$@

clean:
	rm -f gddo

install:
	mkdir -m755 -p $(DESTDIR)$(BINDIR) $(DESTDIR)$(SHAREDIR) \
		$(DESTDIR)$(SHAREDIR)/assets $(DESTDIR)$(SHAREDIR)/templates
	install -m755 gddo $(DESTDIR)$(BINDIR)
	install -m644 assets/* $(DESTDIR)$(SHAREDIR)/assets
	install -m644 templates/* $(DESTDIR)$(SHAREDIR)/templates

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/gddo $(DESTDIR)$(BINDIR)/gddo
	rm -rf $(DESTDIR)$(SHAREDIR)/assets
	rm -rf $(DESTDIR)$(SHAREDIR)/templates

.PHONY: all gddo clean install uninstall
