"""Unit tests for the sidecar's axtree flattener (stdlib unittest; run
from the repo root:  python3 -m unittest discover -s eval/miniwob -p 'sidecar_test.py'

No gym/browser dependency: the flattener is a pure function over the
CDP accessibility-tree node list."""

import importlib.util
import os
import sys
import unittest

_spec = importlib.util.spec_from_file_location(
    "sidecar", os.path.join(os.path.dirname(__file__), "sidecar.py")
)
# Import WITHOUT executing __main__ (the sidecar guards serve_forever
# under __name__ == "__main__").
sidecar = importlib.util.module_from_spec(_spec)
sys.modules["sidecar"] = sidecar
_spec.loader.exec_module(sidecar)


def node(node_id, parent, role="generic", name="", bid=None, ignored=False):
    return {
        "nodeId": node_id,
        "parentId": parent,
        "role": {"type": "internalRole", "value": role},
        "name": {"type": "computedString", "value": name},
        "ignored": ignored,
        **({"browsergym_id": bid} if bid else {}),
    }


def tree(nodes):
    return {"nodes": nodes}


class FlattenAxtreeTest(unittest.TestCase):
    def test_bid_lines_survive_over_budget_in_order(self):
        # A tree over MAX_OBS_CHARS: every bid-carrying line must
        # survive, in tree order, regardless of how much decorative
        # text surrounds it. Mutant this kills: the head-truncation
        # (bid lines at the tail lost) and the all-or-nothing prune
        # (every decorative line dropped even when a few would do).
        max_chars = sidecar.MAX_OBS_CHARS
        nodes = [node(2, None, role="RootWebArea", name="T")]
        # A long decorative run (each StaticText ~40 chars, no bid)...
        filler = "x" * 40
        for i in range(3, 203):
            nodes.append(node(i, 2, role="StaticText", name=filler))
        # ...with the ONLY actionable element at the very END of the tree.
        nodes.append(node(900, 2, role="button", name="Go", bid="e99"))
        flat = sidecar._flatten_axtree(tree(nodes))
        self.assertGreater(len(flat), 0)
        self.assertLessEqual(len(flat), max_chars)
        self.assertIn("[button] 'Go' (e99)", flat, "tail bid line must survive the prune")
        self.assertIn("[RootWebArea] 'T'", flat, "root must survive")
        # Tree order: root before button.
        self.assertLess(flat.index("[RootWebArea]"), flat.index("[button] 'Go' (e99)"))

    def test_prune_stops_when_it_fits_only_a_few_decorative_go(self):
        # Just over budget: the loop must stop once the size fits —
        # decorative lines nearest the HEAD survive. This is the row
        # that dies under the stale-length mutant (all decorative gone).
        max_chars = sidecar.MAX_OBS_CHARS
        nodes = [node(2, None, role="RootWebArea", name="T")]
        head_text = "head filler that must survive the prune"
        tail_text = "tail filler that the prune drops"
        per = len(head_text) + 12
        count = (max_chars // per) + 4  # comfortably over budget
        for i in range(3, 3 + count):
            nodes.append(node(i, 2, role="StaticText", name=head_text))
        nodes.append(node(950, 2, role="StaticText", name=tail_text))
        nodes.append(node(951, 2, role="textbox", name="q", bid="e42"))
        flat = sidecar._flatten_axtree(tree(nodes))
        self.assertIn("[textbox] 'q' (e42)", flat)
        self.assertIn(head_text, flat, "only as many decorative lines as needed may drop")
        self.assertNotIn(tail_text, flat, "the tail decorative line is the one dropped")
        self.assertLessEqual(len(flat), max_chars)

    def test_under_budget_tree_is_untouched(self):
        nodes = [
            node(2, None, role="RootWebArea", name="T"),
            node(3, 2, role="StaticText", name="hello"),
            node(4, 2, role="button", name="B", bid="e7"),
        ]
        flat = sidecar._flatten_axtree(tree(nodes))
        self.assertIn("hello", flat)
        self.assertIn("(e7)", flat)


if __name__ == "__main__":
    unittest.main()
