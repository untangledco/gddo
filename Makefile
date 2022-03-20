PREFIX?=/usr/local
BINDIR?=$(PREFIX)/bin
SHAREDIR?=$(PREFIX)/share/gddo

GOSRC!=find . -name '*.go'
GOSRC+=go.mod go.sum

all: gddo

gddo: $(GOSRC)
	go build \
		-ldflags "-X main.ShareDir=$(SHAREDIR)" \
		-o $@ \
		./cmd/$@

clean:
	rm -f gddo

install: all
	mkdir -m755 -p $(DESTDIR)$(BINDIR) $(DESTDIR)$(SHAREDIR) \
		$(DESTDIR)$(SHAREDIR)/assets $(DESTDIR)$(SHAREDIR)/templates
	install -m755 gddo $(DESTDIR)$(BINDIR)
	install -m644 assets/* $(DESTDIR)$(SHAREDIR)/assets
	install -m644 templates/* $(DESTDIR)$(SHAREDIR)/templates

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/gddo $(DESTDIR)$(BINDIR)/gddo
	rm -rf $(DESTDIR)$(SHAREDIR)/assets
	rm -rf $(DESTDIR)$(SHAREDIR)/templates

.PHONY: all doc clean install uninstall
