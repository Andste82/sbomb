// Package spdx23 is everything about sbomb's SPDX documents that is SPDX 2.3
// spelling: the flat JSON document of packages, files and relationships, the
// element identifiers, the purpose, file type and checksum vocabularies, the
// annotations sbomb's properties travel in, the embedded schema and the checks
// a 2.3 document is held to. What the document states comes from the
// version-neutral model of package mapping, the same model the 3.0.1 renderer
// reads; this package decides only how 2.3 says it, and what to do where 2.3
// cannot say it (section 28.11.11, deviation D57). It is a sibling of the
// 3.0.1 renderer and shares nothing with it but the model.
package spdx23
