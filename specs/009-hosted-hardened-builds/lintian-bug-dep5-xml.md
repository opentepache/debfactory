# Borrador: bug de lintian para Debian (no enviado)

Para: submit@bugs.debian.org

```
Package: lintian
Version: 2.122.0+deb13u1
Severity: important
Tags: patch security

Subject: debian/copyright/dep5 hangs forever on sources shipping an XML
 "billion laughs" file (e.g. libxml2's test/recurse/lol_ep.xml)

Dear Maintainer,

The check debian/copyright/dep5 parses every *.xml file covered by
debian/copyright with XML::LibXML, using the default parser options, to
read /component/metadata_license (AppStream). Default options expand
entities and load external DTDs/entities from local files.

A source package that ships libxml2's own test suite (vendored copies are
common, e.g. Zig dependency caches) contains test/recurse/lol_ep.xml plus
p0.ent..p3.ent: a "billion laughs" built from external parameter
entities. Parsing it never finishes, so lintian runs at 100 % CPU
forever on that source package. We observed > 2.5 h before killing it.

Reproduce (trixie):

  mkdir -p bomb-1.0/test/recurse
  cp libxml2-2.11.5/test/recurse/{lol_ep.xml,p0.ent,p1.ent,p2.ent,p3.ent} \
     bomb-1.0/test/recurse/
  # minimal debian/ with a DEP-5 debian/copyright ("Files: *")
  tar --exclude=debian -czf bomb_1.0.orig.tar.gz bomb-1.0
  dpkg-source -b bomb-1.0
  lintian -C debian/copyright/dep5 bomb_1.0-1.dsc   # never returns

The check does not need entity expansion nor external DTDs to read
metadata_license. With the patch below the same run takes 2 s:

--- a/lib/Lintian/Check/Debian/Copyright/Dep5.pm
+++ b/lib/Lintian/Check/Debian/Copyright/Dep5.pm
@@
-            my $parser = XML::LibXML->new;
+            my $parser = XML::LibXML->new(expand_entities => 0, load_ext_dtd => 0);
             $parser->set_option('no_network', 1);

Other checks that call XML::LibXML on files from the package (e.g.
appstream metadata checks) may deserve the same options.
```

Notas internas:
- debfactory aplica este parche en su imagen `debfactory-lintian`
  (`internal/lint/lintian.go`) y la construcción falla si la línea cambia;
  cuando Debian publique el arreglo se quita el parche.
- Prueba: `internal/lint/bomb_e2e_test.go` con `testdata/lint/recurse/`.
