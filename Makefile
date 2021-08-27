.POSIX:

VERSION=0.0.0

PREFIX?=/usr/local
BINDIR?=$(PREFIX)/bin
SHAREDIR?=$(PREFIX)/share/gddo

GOSRC!=find . -name '*.go'
GOSRC+=go.mod go.sum

all: gddo-server gddo-admin

gddo-server: $(GOSRC)
	go build \
		-ldflags "-X main.Version=$(VERSION) -X main.ShareDir=$(SHAREDIR)" \
		-o $@ \
		./cmd/$@

gddo-admin: $(GOSRC)
	go build \
		-ldflags "-X main.Version=$(VERSION) -X main.ShareDir=$(SHAREDIR)" \
		-o $@ \
		./cmd/$@

clean:
	rm gddo-server gddo-admin

install: all
	mkdir -m755 -p $(DESTDIR)$(BINDIR) $(DESTDIR)$(SHAREDIR) \
		$(DESTDIR)$(SHAREDIR)/assets $(DESTDIR)$(SHAREDIR)/templates
	install -m755 gddo-server gddo-admin $(DESTDIR)$(BINDIR)
	install -m644 assets/* $(DESTDIR)$(SHAREDIR)/assets
	install -m644 templates/* $(DESTDIR)$(SHAREDIR)/templates

uninstall:
	rm $(DESTDIR)$(BINDIR)/gddo-server $(DESTDIR)$(BINDIR)/gddo-admin
	rm -r $(DESTDIR)$(SHAREDIR)/assets
	rm -r $(DESTDIR)$(SHAREDIR)/templates

.PHONY: all doc clean install uninstall
