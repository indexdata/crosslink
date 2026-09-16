import copy
import json
import sys
import threading
import unittest
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, unquote, urlsplit

sys.path.insert(0, str(Path(__file__).parent))

from seed_directory import (  # noqa: E402
    DirectoryClient,
    PERMISSIONS,
    SeedError,
    Seeder,
    build_catalog,
    clean_entry,
)


NETWORK_ID = "10000000-0000-0000-0000-000000000001"
LOAN_TIER_ID = "20000000-0000-0000-0000-000000000001"
COPY_TIER_ID = "20000000-0000-0000-0000-000000000002"


def fixture():
    result = []
    for index in range(1, 4):
        result.append(
            {
                "id": f"30000000-0000-0000-0000-00000000000{index}",
                "name": f"rs{index}",
                "type": "Institution",
                "symbols": [
                    {
                        "id": str(uuid.uuid4()),
                        "authority": "ISIL",
                        "symbol": f"US-RS{index}",
                    }
                ],
                "addresses": [
                    {
                        "id": str(uuid.uuid4()),
                        "entry": "source-entry",
                        "type": "Shipping",
                        "addressComponents": [
                            {
                                "id": str(uuid.uuid4()),
                                "address": "source-address",
                                "seq": 0,
                                "type": "Locality",
                                "value": "Test",
                            }
                        ],
                    }
                ],
                "tiers": [
                    {
                        "id": LOAN_TIER_ID,
                        "name": "Loan",
                        "type": "loan",
                        "level": "standard",
                        "cost": 0.0,
                    },
                    {
                        "id": COPY_TIER_ID,
                        "name": "Copy",
                        "type": "copy",
                        "level": "standard",
                        "cost": 0.0,
                    },
                ],
                "networks": [
                    {"id": NETWORK_ID, "name": "Shared", "priority": 1}
                ],
            }
        )
    return result


class DirectoryState:
    def __init__(self):
        self.next_id = 10
        self.entries = {}
        self.tiers = {}
        self.networks = {}
        self.entry_tiers = {}
        self.entry_networks = {}
        self.permissions = []
        self.tenants = []

    def new_id(self):
        self.next_id += 1
        return str(uuid.UUID(int=self.next_id))


class Handler(BaseHTTPRequestHandler):
    def log_message(self, _format, *_args):
        pass

    @property
    def state(self):
        return self.server.state

    def send_json(self, status, value):
        body = json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def parts(self):
        return [unquote(value) for value in urlsplit(self.path).path.strip("/").split("/")]

    def page(self, values):
        query = parse_qs(urlsplit(self.path).query)
        offset = int(query.get("offset", [0])[0])
        limit = int(query.get("limit", [1000])[0])
        return list(values)[offset : offset + limit]

    def do_GET(self):
        self.state.permissions.append(self.headers.get("X-Okapi-Permissions"))
        self.state.tenants.append(self.headers.get("X-Okapi-Tenant"))
        parts = self.parts()
        collections = {
            "entries": self.state.entries,
            "tiers": self.state.tiers,
            "networks": self.state.networks,
        }
        if len(parts) == 2 and parts[0] == "directory" and parts[1] in collections:
            items = self.page(collections[parts[1]].values())
            self.send_json(200, {"items": items, "about": {"count": len(items)}})
            return
        if len(parts) == 4 and parts[:3] == ["directory", "entries", "by-symbol"]:
            for entry in self.state.entries.values():
                symbols = {
                    f"{item['authority']}:{item['symbol']}"
                    for item in entry.get("symbols", [])
                }
                if parts[3] in symbols:
                    self.send_json(200, entry)
                    return
            self.send_error(404)
            return
        if len(parts) == 5 and parts[:3] == ["directory", "entries", "by-id"]:
            entry_id, kind = parts[3:]
            if kind == "tiers":
                items = [self.state.tiers[item] for item in self.state.entry_tiers.get(entry_id, set())]
            elif kind == "networks":
                items = [
                    dict(self.state.networks[item], priority=priority)
                    for item, priority in self.state.entry_networks.get(entry_id, {}).items()
                ]
            else:
                self.send_error(404)
                return
            self.send_json(200, {"items": items, "about": {"count": len(items)}})
            return
        self.send_error(404)

    def do_POST(self):
        self.state.permissions.append(self.headers.get("X-Okapi-Permissions"))
        self.state.tenants.append(self.headers.get("X-Okapi-Tenant"))
        length = int(self.headers.get("Content-Length", "0"))
        payload = json.loads(self.rfile.read(length))
        parts = self.parts()
        created_id = self.state.new_id()
        if parts == ["directory", "entries"]:
            self.state.entries[created_id] = dict(payload, id=created_id)
        elif parts == ["directory", "tiers"]:
            self.state.tiers[created_id] = dict(payload, id=created_id)
        elif parts == ["directory", "networks"]:
            self.state.networks[created_id] = dict(payload, id=created_id)
        elif len(parts) == 5 and parts[:3] == ["directory", "entries", "by-id"]:
            entry_id, kind = parts[3:]
            if kind == "tiers":
                self.state.entry_tiers.setdefault(entry_id, set()).add(payload["id"])
            elif kind == "networks":
                self.state.entry_networks.setdefault(entry_id, {})[payload["id"]] = payload["priority"]
            else:
                self.send_error(404)
                return
        else:
            self.send_error(404)
            return
        self.send_json(201, {"id": created_id})


class SeederTest(unittest.TestCase):
    def setUp(self):
        self.state = DirectoryState()
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.state = self.state
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.client = DirectoryClient(f"http://127.0.0.1:{self.server.server_port}")

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def test_seed_remaps_ids_and_verifies_result(self):
        seeder = Seeder(self.client, fixture(), "Test Consortium")
        seeder.wait_until_ready(1, 0)
        seeder.seed()
        seeder.verify()

        self.assertEqual(len(self.state.entries), 4)
        self.assertEqual(len(self.state.tiers), 2)
        self.assertEqual(len(self.state.networks), 1)
        self.assertEqual(set(self.state.permissions), {PERMISSIONS})
        self.assertEqual(set(self.state.tenants), {None})
        institutions = [item for item in self.state.entries.values() if item["type"] == "Institution"]
        consortium = next(item for item in self.state.entries.values() if item["type"] == "Consortium")
        self.assertTrue(all(item["parent"] == consortium["id"] for item in institutions))
        self.assertTrue(
            all(next(iter(self.state.entry_networks[item["id"]].values())) == 1 for item in institutions)
        )

    def test_seed_rejects_nonempty_destination_before_writes(self):
        existing_id = self.state.new_id()
        self.state.entries[existing_id] = {"id": existing_id, "name": "Existing"}
        with self.assertRaisesRegex(SeedError, "not empty"):
            Seeder(self.client, fixture(), "Test Consortium").seed()
        self.assertEqual(len(self.state.entries), 1)

    def test_conflicting_duplicate_catalog_definition_is_rejected(self):
        data = fixture()
        data[1]["networks"][0]["name"] = "Another name"
        with self.assertRaisesRegex(SeedError, "conflicting names"):
            build_catalog(data, "networks")

    def test_clean_entry_removes_readonly_nested_ids(self):
        payload = clean_entry(copy.deepcopy(fixture()[0]), "consortium-id")
        self.assertNotIn("id", payload)
        self.assertNotIn("networks", payload)
        self.assertNotIn("tiers", payload)
        self.assertNotIn("id", payload["symbols"][0])
        self.assertNotIn("id", payload["addresses"][0])
        self.assertNotIn("entry", payload["addresses"][0])
        component = payload["addresses"][0]["addressComponents"][0]
        self.assertNotIn("id", component)
        self.assertNotIn("address", component)


if __name__ == "__main__":
    unittest.main()
