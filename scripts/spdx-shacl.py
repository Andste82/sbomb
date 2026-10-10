#!/usr/bin/env python3
"""Validate SPDX 3.0.1 JSON-LD documents against the SPDX SHACL model.

Usage: spdx-shacl.py MODEL.ttl CONTEXT.jsonld DOCUMENT.json [DOCUMENT.json ...]

This is the semantic half of the validation the SPDX project describes
(serialization/jsonld/validation.md); the structural half is the JSON schema,
which check-jsonschema covers and sbomb itself embeds. The SHACL model is not
run by sbomb at run time (deviation D51): it needs an RDF stack, and what it
checks beyond the schema is re-stated as Go rules in the two validation tiers.
CI runs it on every SPDX golden so that those Go rules are held to the model
they paraphrase rather than to themselves.

Three choices are deliberate and each has a reason:

* The context is read from a local copy. Every document names the published
  context by URL, and a CI job that fetches it on each run fails when
  spdx.org is slow -- for a reason that says nothing about the document. The
  local file is substituted for the URL before parsing, which is the same
  expansion a resolver would perform.

* No RDFS inference (inference=None), which is also the recipe of
  validation.md (`pyshacl --shacl model.ttl --ont-graph model.ttl doc`).
  Inference materialises the abstract superclasses -- every Package becomes an
  Artifact and an Element as well -- and the model's shapes for abstract
  classes reject any instance of them, so every document would fail.

* Every result counts, warnings included (pyshacl's default). A reference to
  an element the document does not define fails the model's sh:class
  constraints, because nothing states the class of an IRI no node defines;
  that is legal SPDX for a document that refers to elements published
  elsewhere, but sbomb's own documents define every element they name (tier
  b), so on a golden it is a defect.

Exit status: 0 when every document conforms, 1 when any does not, 2 on usage
or a document that cannot be read.
"""

import json
import sys

import pyshacl
import rdflib

CONTEXT_URL = "https://spdx.org/rdf/3.0.1/spdx-context.jsonld"


def load_document(path, context):
    with open(path, encoding="utf-8") as handle:
        document = json.load(handle)
    if document.get("@context") != CONTEXT_URL:
        raise ValueError(f"{path}: @context is not {CONTEXT_URL}")
    document["@context"] = context
    return rdflib.Graph().parse(data=json.dumps(document), format="json-ld")


def main(argv):
    if len(argv) < 4:
        print(__doc__.strip().splitlines()[2], file=sys.stderr)
        return 2
    model_path, context_path, documents = argv[1], argv[2], argv[3:]
    model = rdflib.Graph().parse(model_path, format="turtle")
    with open(context_path, encoding="utf-8") as handle:
        context = json.load(handle)["@context"]

    status = 0
    for path in documents:
        try:
            data = load_document(path, context)
        except (OSError, ValueError) as error:
            print(f"ERROR {error}", file=sys.stderr)
            return 2
        conforms, _, report = pyshacl.validate(
            data, shacl_graph=model, ont_graph=model, inference=None
        )
        if conforms:
            print(f"conforms: {path}")
        else:
            status = 1
            print(f"DOES NOT CONFORM: {path}")
            print(report)
    return status


if __name__ == "__main__":
    sys.exit(main(sys.argv))
