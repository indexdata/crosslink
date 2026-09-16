import contextlib
import io
import json
import os
import sys
import tempfile
import threading
import unittest
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch
from urllib.parse import parse_qs, unquote, urlsplit

sys.path.insert(0, str(Path(__file__).parent))

import load_directory_data  # noqa: E402
from load_directory_data import (  # noqa: E402
    ApiError,
    DirectoryClient,
    DirectoryLoader,
    LoadError,
    ResultManifest,
    normalize_localhost_replacement,
    replace_localhost_urls,
    validate_dataset,
)


CONSORTIUM_ID = "00000000-0000-0000-0000-000000000001"
INSTITUTION_ID = "10000000-0000-0000-0000-000000000001"
BRANCH_ID = "10000000-0000-0000-0000-000000000002"
TIER_ID = "20000000-0000-0000-0000-000000000001"
NETWORK_ID = "30000000-0000-0000-0000-000000000001"
TOKEN = "secret-okapi-token"
TENANT = "example_tenant"


def fixture():
    tier = {
        "id": TIER_ID,
        "name": "Standard loan",
        "consortium": "90000000-0000-0000-0000-000000000001",
        "type": "loan",
        "level": "standard",
        "cost": 0.0,
    }
    network = {
        "id": NETWORK_ID,
        "name": "Shared network",
        "consortium": "90000000-0000-0000-0000-000000000001",
    }
    return [
        {
            "id": INSTITUTION_ID,
            "name": "Main institution",
            "type": "Institution",
            "parent": "90000000-0000-0000-0000-000000000001",
            "symbols": [
                {
                    "id": str(uuid.uuid4()),
                    "authority": "ISIL",
                    "symbol": "US-MAIN",
                }
            ],
            "endpoints": [
                {
                    "id": str(uuid.uuid4()),
                    "name": "NCIP",
                    "type": "NCIP",
                    "address": "https://example.test/ncip",
                }
            ],
            "lmsConfig": {
                "address": "https://example.test/ncip",
                "fromAgency": "MAIN",
                "toAgency": "MAIN",
            },
            "holdingsPolicy": {"locations": []},
            "catalogConfig": {"sru": {"address": "https://example.test/sru"}},
            "tiers": [tier],
            "networks": [dict(network, priority=7)],
        },
        {
            "id": BRANCH_ID,
            "name": "Main branch",
            "description": "A branch",
            "type": "Branch",
            "parent": INSTITUTION_ID,
            "tiers": [tier],
            "networks": [network],
        },
    ]


class DirectoryState:
    def __init__(self):
        self.next_id = 100
        self.entries = {
            CONSORTIUM_ID: {
                "id": CONSORTIUM_ID,
                "name": "Target consortium",
                "type": "Consortium",
            }
        }
        self.tiers = {}
        self.networks = {}
        self.entry_tiers = {}
        self.entry_networks = {}
        self.requests = []
        self.fail_path = None
        self.fail_body = "injected failure"

    def new_id(self):
        self.next_id += 1
        return str(uuid.UUID(int=self.next_id))


class Handler(BaseHTTPRequestHandler):
    def log_message(self, _format, *_args):
        pass

    @property
    def state(self):
        return self.server.state

    def parts(self):
        return [unquote(item) for item in urlsplit(self.path).path.strip("/").split("/")]

    def send_json(self, status, value):
        body = json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def send_failure(self):
        body = self.state.fail_body.encode()
        self.send_response(500)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("X-Test-Request-Id", "request-123")
        self.send_header("Set-Cookie", f"session={TOKEN}")
        self.end_headers()
        self.wfile.write(body)

    def record(self, payload=None):
        self.state.requests.append(
            {
                "method": self.command,
                "path": urlsplit(self.path).path,
                "query": parse_qs(urlsplit(self.path).query),
                "token": self.headers.get("X-Okapi-Token"),
                "tenant": self.headers.get("X-Okapi-Tenant"),
                "permissions": self.headers.get("X-Okapi-Permissions"),
                "payload": payload,
            }
        )

    def page(self, values):
        query = parse_qs(urlsplit(self.path).query)
        offset = int(query.get("offset", [0])[0])
        limit = int(query.get("limit", [1000])[0])
        return list(values)[offset : offset + limit]

    def do_GET(self):
        self.record()
        if self.state.fail_path == urlsplit(self.path).path:
            self.send_failure()
            return
        parts = self.parts()
        if len(parts) == 4 and parts[:3] == ["directory", "entries", "by-id"]:
            item = self.state.entries.get(parts[3])
            if item is None:
                self.send_error(404)
            else:
                self.send_json(200, item)
            return
        if len(parts) == 3 and parts[:2] == ["directory", "tiers"]:
            item = self.state.tiers.get(parts[2])
            if item is None:
                self.send_error(404)
            else:
                self.send_json(200, item)
            return
        if len(parts) == 3 and parts[:2] == ["directory", "networks"]:
            item = self.state.networks.get(parts[2])
            if item is None:
                self.send_error(404)
            else:
                self.send_json(200, item)
            return
        if len(parts) == 2 and parts[0] == "directory":
            collections = {
                "entries": self.state.entries,
                "tiers": self.state.tiers,
                "networks": self.state.networks,
            }
            values = collections.get(parts[1])
            if values is not None:
                items = self.page(values.values())
                self.send_json(200, {"items": items, "about": {"count": len(values)}})
                return
        if len(parts) == 5 and parts[:3] == ["directory", "entries", "by-id"]:
            entry_id, kind = parts[3:]
            if kind == "tiers":
                if self.state.entries[entry_id]["type"] == "Consortium":
                    items = [
                        tier
                        for tier in self.state.tiers.values()
                        if tier["consortium"] == entry_id
                    ]
                else:
                    items = [
                        self.state.tiers[tier_id]
                        for tier_id in self.state.entry_tiers.get(entry_id, [])
                    ]
            elif kind == "networks":
                if self.state.entries[entry_id]["type"] == "Consortium":
                    items = [
                        network
                        for network in self.state.networks.values()
                        if network["consortium"] == entry_id
                    ]
                else:
                    memberships = self.state.entry_networks.get(entry_id, {})
                    items = [
                        dict(self.state.networks[network_id], priority=priority)
                        for network_id, priority in memberships.items()
                    ]
            else:
                self.send_error(404)
                return
            self.send_json(200, {"items": self.page(items), "about": {"count": len(items)}})
            return
        self.send_error(404)

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        payload = json.loads(self.rfile.read(length))
        self.record(payload)
        path = urlsplit(self.path).path
        if self.state.fail_path == path:
            self.send_failure()
            return

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
                self.state.entry_tiers.setdefault(entry_id, []).append(payload["id"])
            elif kind == "networks":
                memberships = self.state.entry_networks.setdefault(entry_id, {})
                memberships[payload["id"]] = payload.get("priority", 0)
            else:
                self.send_error(404)
                return
        else:
            self.send_error(404)
            return
        self.send_json(201, {"id": created_id})

    def do_PATCH(self):
        length = int(self.headers.get("Content-Length", "0"))
        payload = json.loads(self.rfile.read(length))
        self.record(payload)
        path = urlsplit(self.path).path
        if self.state.fail_path == path:
            self.send_failure()
            return
        parts = self.parts()
        if len(parts) != 4 or parts[:3] != ["directory", "entries", "by-id"]:
            self.send_error(404)
            return
        entry = self.state.entries.get(parts[3])
        if entry is None:
            self.send_error(404)
            return
        entry.update(payload)
        self.send_response(204)
        self.end_headers()

    def do_DELETE(self):
        self.record()
        path = urlsplit(self.path).path
        if self.state.fail_path == path:
            self.send_failure()
            return
        parts = self.parts()
        if len(parts) != 4 or parts[:3] != ["directory", "entries", "by-id"]:
            self.send_error(404)
            return
        entry_id = parts[3]
        if entry_id not in self.state.entries:
            self.send_error(404)
            return
        if any(
            entry.get("parent") == entry_id
            for entry in self.state.entries.values()
        ):
            self.send_error(409)
            return
        del self.state.entries[entry_id]
        self.state.entry_tiers.pop(entry_id, None)
        self.state.entry_networks.pop(entry_id, None)
        self.send_response(204)
        self.end_headers()


class LoaderTest(unittest.TestCase):
    def setUp(self):
        self.state = DirectoryState()
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.state = self.state
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.base_url = f"http://127.0.0.1:{self.server.server_port}"
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def manifest(
        self,
        name="result.json",
        allow_existing=False,
        piecemeal=False,
        delete_entries=False,
    ):
        return ResultManifest(
            Path(self.temporary.name) / name,
            self.base_url + "/directory",
            TENANT,
            CONSORTIUM_ID,
            Path("fixture.json"),
            False,
            allow_existing=allow_existing,
            piecemeal=piecemeal,
            delete_entries=delete_entries,
        )

    def seed(self, raw_fixture):
        manifest = self.manifest(name=f"seed-{uuid.uuid4()}.json")
        manifest.write()
        loader = DirectoryLoader(
            DirectoryClient(self.base_url, TOKEN, TENANT),
            validate_dataset(raw_fixture),
            CONSORTIUM_ID,
            manifest,
        )
        loader.preflight()
        loader.load()
        loader.verify()

    def test_load_remaps_graph_authenticates_and_verifies(self):
        dataset = validate_dataset(fixture())
        manifest = self.manifest()
        manifest.write()
        loader = DirectoryLoader(
            DirectoryClient(self.base_url, TOKEN, TENANT),
            dataset,
            CONSORTIUM_ID,
            manifest,
        )

        counts = loader.preflight()
        loader.load()
        loader.verify()
        manifest.finish("succeeded")

        self.assertEqual(counts["entries"], 2)
        self.assertEqual(len(self.state.tiers), 1)
        self.assertEqual(len(self.state.networks), 1)
        created_entries = [
            item for item in self.state.entries.values() if item["type"] != "Consortium"
        ]
        institution = next(item for item in created_entries if item["type"] == "Institution")
        branch = next(item for item in created_entries if item["type"] == "Branch")
        self.assertEqual(institution["parent"], CONSORTIUM_ID)
        self.assertEqual(branch["parent"], institution["id"])
        self.assertNotIn("id", institution["symbols"][0])
        self.assertNotIn("id", institution["endpoints"][0])
        self.assertEqual(set(item["token"] for item in self.state.requests), {TOKEN})
        self.assertEqual(set(item["tenant"] for item in self.state.requests), {TENANT})
        self.assertEqual(set(item["permissions"] for item in self.state.requests), {None})

        manifest_data = json.loads(manifest.path.read_text())
        self.assertEqual(manifest_data["status"], "succeeded")
        self.assertEqual(manifest_data["tenant"], TENANT)
        self.assertEqual(
            manifest_data["maps"]["entries"][INSTITUTION_ID], institution["id"]
        )
        self.assertEqual(manifest_data["maps"]["entries"][BRANCH_ID], branch["id"])
        priorities = sorted(
            item["priority"] for item in manifest_data["created"]["entryNetworks"]
        )
        self.assertEqual(priorities, [0, 7])
        self.assertNotIn(TOKEN, manifest.path.read_text())

        post_paths = [
            item["path"] for item in self.state.requests if item["method"] == "POST"
        ]
        self.assertEqual(post_paths[:4], [
            "/directory/tiers",
            "/directory/networks",
            "/directory/entries",
            "/directory/entries",
        ])

    def test_piecemeal_creates_entry_then_patches_each_nested_field(self):
        manifest = self.manifest(piecemeal=True)
        manifest.write()
        loader = DirectoryLoader(
            DirectoryClient(self.base_url, TOKEN, TENANT),
            validate_dataset(fixture()),
            CONSORTIUM_ID,
            manifest,
            piecemeal=True,
        )

        loader.preflight()
        loader.load()
        loader.verify()

        entry_posts = [
            item
            for item in self.state.requests
            if item["method"] == "POST" and item["path"] == "/directory/entries"
        ]
        self.assertEqual(len(entry_posts), 2)
        deferred_fields = {
            "symbols",
            "lmsConfig",
            "endpoints",
            "holdingsPolicy",
            "catalogConfig",
        }
        self.assertTrue(deferred_fields.isdisjoint(entry_posts[0]["payload"]))

        institution_id = next(
            item["id"]
            for item in self.state.entries.values()
            if item["type"] == "Institution"
        )
        patches = [
            item
            for item in self.state.requests
            if item["method"] == "PATCH"
            and item["path"] == f"/directory/entries/by-id/{institution_id}"
        ]
        self.assertEqual(
            [next(iter(item["payload"])) for item in patches],
            [
                "symbols",
                "lmsConfig",
                "endpoints",
                "holdingsPolicy",
                "catalogConfig",
            ],
        )
        self.assertTrue(
            deferred_fields.issubset(self.state.entries[institution_id].keys())
        )
        result = json.loads(manifest.path.read_text())
        self.assertTrue(result["piecemeal"])
        self.assertEqual(
            [item["field"] for item in result["patchedEntries"]],
            [
                "symbols",
                "lmsConfig",
                "endpoints",
                "holdingsPolicy",
                "catalogConfig",
            ],
        )

    def test_collision_fails_before_writes(self):
        self.state.entries[str(uuid.uuid4())] = {
            "id": str(uuid.uuid4()),
            "name": "Elsewhere",
            "type": "Institution",
            "symbols": [{"authority": "isil", "symbol": "us-main"}],
        }
        manifest = self.manifest()
        manifest.write()
        loader = DirectoryLoader(
            DirectoryClient(self.base_url, TOKEN, TENANT),
            validate_dataset(fixture()),
            CONSORTIUM_ID,
            manifest,
        )
        with self.assertRaisesRegex(LoadError, "entry symbol ISIL:US-MAIN"):
            loader.preflight()
        self.assertFalse(any(item["method"] == "POST" for item in self.state.requests))

    def test_get_error_reports_request_without_exposing_token(self):
        self.state.fail_path = "/directory/entries"
        client = DirectoryClient(self.base_url, TOKEN, TENANT)
        with self.assertRaises(ApiError) as raised:
            client.get("/entries")
        message = str(raised.exception)
        self.assertIn(
            f"Attempted request: GET {self.base_url}/directory/entries", message
        )
        self.assertIn('"X-Okapi-Tenant": "example_tenant"', message)
        self.assertIn('"X-Okapi-Token": "[REDACTED]"', message)
        self.assertIn("Request content: <none>", message)
        self.assertIn('"X-Test-Request-Id": "request-123"', message)
        self.assertIn('"Set-Cookie": "[REDACTED]"', message)
        self.assertNotIn(TOKEN, message)

    def test_allow_existing_reuses_tier_and_network_by_name(self):
        existing_tier_id = str(uuid.uuid4())
        existing_network_id = str(uuid.uuid4())
        self.state.tiers[existing_tier_id] = {
            "id": existing_tier_id,
            "name": "Standard loan",
            "consortium": CONSORTIUM_ID,
            "type": "loan",
            "level": "standard",
            "cost": 0.0,
        }
        self.state.networks[existing_network_id] = {
            "id": existing_network_id,
            "name": "Shared network",
            "consortium": CONSORTIUM_ID,
        }
        manifest = self.manifest(allow_existing=True)
        manifest.write()
        loader = DirectoryLoader(
            DirectoryClient(self.base_url, TOKEN, TENANT),
            validate_dataset(fixture()),
            CONSORTIUM_ID,
            manifest,
            allow_existing=True,
        )

        counts = loader.preflight()
        loader.load()
        loader.verify()

        self.assertEqual(counts["reusedTiers"], 1)
        self.assertEqual(counts["reusedNetworks"], 1)
        catalog_posts = {
            item["path"]
            for item in self.state.requests
            if item["method"] == "POST"
            and item["path"] in {"/directory/tiers", "/directory/networks"}
        }
        self.assertEqual(catalog_posts, set())
        result = json.loads(manifest.path.read_text())
        self.assertTrue(result["allowExisting"])
        self.assertEqual(result["maps"]["tiers"][TIER_ID], existing_tier_id)
        self.assertEqual(result["maps"]["networks"][NETWORK_ID], existing_network_id)
        self.assertEqual(result["reused"]["tiers"][0]["id"], existing_tier_id)
        self.assertEqual(result["reused"]["networks"][0]["id"], existing_network_id)

    def test_allow_existing_reuses_entries_and_memberships(self):
        existing_institution_id = str(uuid.uuid4())
        existing_branch_id = str(uuid.uuid4())
        existing_tier_id = str(uuid.uuid4())
        existing_network_id = str(uuid.uuid4())
        self.state.entries[existing_institution_id] = {
            "id": existing_institution_id,
            "name": "Main institution",
            "type": "Institution",
            "parent": CONSORTIUM_ID,
            "symbols": [{"authority": "ISIL", "symbol": "US-MAIN"}],
        }
        self.state.entries[existing_branch_id] = {
            "id": existing_branch_id,
            "name": "Main branch",
            "type": "Branch",
            "parent": existing_institution_id,
        }
        self.state.tiers[existing_tier_id] = {
            "id": existing_tier_id,
            "name": "Standard loan",
            "consortium": CONSORTIUM_ID,
        }
        self.state.networks[existing_network_id] = {
            "id": existing_network_id,
            "name": "Shared network",
            "consortium": CONSORTIUM_ID,
        }
        for entry_id in (existing_institution_id, existing_branch_id):
            self.state.entry_tiers[entry_id] = [existing_tier_id]
        self.state.entry_networks[existing_institution_id] = {
            existing_network_id: 7
        }
        self.state.entry_networks[existing_branch_id] = {existing_network_id: 0}

        manifest = self.manifest(allow_existing=True)
        manifest.write()
        loader = DirectoryLoader(
            DirectoryClient(self.base_url, TOKEN, TENANT),
            validate_dataset(fixture()),
            CONSORTIUM_ID,
            manifest,
            allow_existing=True,
        )

        counts = loader.preflight()
        loader.load()
        loader.verify()

        self.assertEqual(counts["reusedEntries"], 2)
        posts = [item for item in self.state.requests if item["method"] == "POST"]
        self.assertEqual(posts, [])
        result = json.loads(manifest.path.read_text())
        self.assertEqual(len(result["reused"]["entries"]), 2)
        self.assertEqual(len(result["reused"]["entryTiers"]), 2)
        self.assertEqual(len(result["reused"]["entryNetworks"]), 2)
        self.assertEqual(
            result["maps"]["entries"][INSTITUTION_ID], existing_institution_id
        )
        self.assertEqual(result["maps"]["entries"][BRANCH_ID], existing_branch_id)

    def test_delete_entries_removes_duplicates_child_first_then_loads(self):
        existing_institution_id = str(uuid.uuid4())
        existing_branch_id = str(uuid.uuid4())
        existing_tier_id = str(uuid.uuid4())
        existing_network_id = str(uuid.uuid4())
        self.state.entries[existing_institution_id] = {
            "id": existing_institution_id,
            "name": "Main institution",
            "type": "Institution",
            "parent": CONSORTIUM_ID,
            "symbols": [{"authority": "ISIL", "symbol": "US-MAIN"}],
        }
        self.state.entries[existing_branch_id] = {
            "id": existing_branch_id,
            "name": "Main branch",
            "type": "Branch",
            "parent": existing_institution_id,
        }
        self.state.tiers[existing_tier_id] = {
            "id": existing_tier_id,
            "name": "Standard loan",
            "consortium": CONSORTIUM_ID,
        }
        self.state.networks[existing_network_id] = {
            "id": existing_network_id,
            "name": "Shared network",
            "consortium": CONSORTIUM_ID,
        }

        manifest = self.manifest(allow_existing=True, delete_entries=True)
        manifest.write()
        loader = DirectoryLoader(
            DirectoryClient(self.base_url, TOKEN, TENANT),
            validate_dataset(fixture()),
            CONSORTIUM_ID,
            manifest,
            allow_existing=True,
            delete_entries=True,
        )

        counts = loader.preflight()
        loader.load()
        loader.verify()

        self.assertEqual(counts["entriesToDelete"], 2)
        self.assertEqual(counts["reusedEntries"], 0)
        self.assertEqual(counts["reusedTiers"], 1)
        self.assertEqual(counts["reusedNetworks"], 1)
        self.assertNotIn(existing_institution_id, self.state.entries)
        self.assertNotIn(existing_branch_id, self.state.entries)

        mutations = [
            item
            for item in self.state.requests
            if item["method"] in {"DELETE", "POST", "PATCH"}
        ]
        self.assertEqual(
            [(item["method"], item["path"]) for item in mutations[:2]],
            [
                ("DELETE", f"/directory/entries/by-id/{existing_branch_id}"),
                ("DELETE", f"/directory/entries/by-id/{existing_institution_id}"),
            ],
        )
        catalog_posts = {
            item["path"]
            for item in mutations
            if item["method"] == "POST"
            and item["path"] in {"/directory/tiers", "/directory/networks"}
        }
        self.assertEqual(catalog_posts, set())

        result = json.loads(manifest.path.read_text())
        self.assertTrue(result["deleteEntries"])
        self.assertEqual(
            [item["id"] for item in result["deletedEntries"]],
            [existing_branch_id, existing_institution_id],
        )
        self.assertNotEqual(
            result["maps"]["entries"][INSTITUTION_ID], existing_institution_id
        )
        self.assertNotEqual(result["maps"]["entries"][BRANCH_ID], existing_branch_id)

    def test_delete_entries_rejects_duplicate_parent_with_unmatched_child(self):
        existing_institution_id = str(uuid.uuid4())
        unrelated_branch_id = str(uuid.uuid4())
        self.state.entries[existing_institution_id] = {
            "id": existing_institution_id,
            "name": "Main institution",
            "type": "Institution",
            "parent": CONSORTIUM_ID,
            "symbols": [{"authority": "ISIL", "symbol": "US-MAIN"}],
        }
        self.state.entries[unrelated_branch_id] = {
            "id": unrelated_branch_id,
            "name": "Unrelated branch",
            "type": "Branch",
            "parent": existing_institution_id,
        }
        manifest = self.manifest(delete_entries=True)
        manifest.write()
        loader = DirectoryLoader(
            DirectoryClient(self.base_url, TOKEN, TENANT),
            validate_dataset(fixture()),
            CONSORTIUM_ID,
            manifest,
            delete_entries=True,
        )

        with self.assertRaisesRegex(LoadError, "non-duplicate children"):
            loader.preflight()
        self.assertFalse(
            any(
                item["method"] in {"DELETE", "POST", "PATCH"}
                for item in self.state.requests
            )
        )

    def test_existing_catalog_resource_fails_without_allow_existing(self):
        existing_tier_id = str(uuid.uuid4())
        self.state.tiers[existing_tier_id] = {
            "id": existing_tier_id,
            "name": "Standard loan",
            "consortium": CONSORTIUM_ID,
        }
        manifest = self.manifest()
        manifest.write()
        loader = DirectoryLoader(
            DirectoryClient(self.base_url, TOKEN, TENANT),
            validate_dataset(fixture()),
            CONSORTIUM_ID,
            manifest,
        )

        with self.assertRaisesRegex(LoadError, "tier name 'Standard loan'"):
            loader.preflight()
        self.assertFalse(any(item["method"] == "POST" for item in self.state.requests))

    def test_preflight_paginates(self):
        self.state.entries[str(uuid.uuid4())] = {
            "id": str(uuid.uuid4()),
            "name": "Unrelated",
            "type": "Institution",
        }
        manifest = self.manifest()
        manifest.write()
        loader = DirectoryLoader(
            DirectoryClient(self.base_url, TOKEN, TENANT),
            validate_dataset(fixture()),
            CONSORTIUM_ID,
            manifest,
        )
        with patch.object(load_directory_data, "PAGE_SIZE", 1):
            loader.preflight()
        entry_gets = [
            item for item in self.state.requests
            if item["method"] == "GET" and item["path"] == "/directory/entries"
        ]
        self.assertGreaterEqual(len(entry_gets), 3)
        self.assertEqual(entry_gets[0]["query"]["offset"], ["0"])
        self.assertEqual(entry_gets[1]["query"]["offset"], ["1"])

    def test_main_dry_run_does_not_write_to_directory(self):
        fixture_path = Path(self.temporary.name) / "fixture.json"
        result_path = Path(self.temporary.name) / "dry-run.json"
        fixture_path.write_text(json.dumps(fixture()))
        with patch.dict(os.environ, {"DIRECTORY_TOKEN": TOKEN}):
            status = load_directory_data.main([
                "--base-url", self.base_url,
                "--tenant", TENANT,
                "--consortium-id", CONSORTIUM_ID,
                "--fixture", str(fixture_path),
                "--result", str(result_path),
                "--dry-run",
            ])
        self.assertEqual(status, 0)
        self.assertFalse(any(item["method"] == "POST" for item in self.state.requests))
        self.assertEqual(json.loads(result_path.read_text())["status"], "dry-run")

    def test_main_delete_entries_dry_run_only_reports_matches(self):
        existing_institution_id = str(uuid.uuid4())
        self.state.entries[existing_institution_id] = {
            "id": existing_institution_id,
            "name": "Main institution",
            "type": "Institution",
            "parent": CONSORTIUM_ID,
            "symbols": [{"authority": "ISIL", "symbol": "US-MAIN"}],
        }
        fixture_path = Path(self.temporary.name) / "fixture.json"
        result_path = Path(self.temporary.name) / "delete-dry-run.json"
        fixture_path.write_text(json.dumps(fixture()))

        with patch.dict(os.environ, {"DIRECTORY_TOKEN": TOKEN}):
            status = load_directory_data.main([
                "--base-url", self.base_url,
                "--tenant", TENANT,
                "--consortium-id", CONSORTIUM_ID,
                "--fixture", str(fixture_path),
                "--result", str(result_path),
                "--delete-entries",
                "--dry-run",
            ])

        self.assertEqual(status, 0)
        self.assertIn(existing_institution_id, self.state.entries)
        self.assertFalse(
            any(
                item["method"] in {"DELETE", "POST", "PATCH"}
                for item in self.state.requests
            )
        )
        result = json.loads(result_path.read_text())
        self.assertTrue(result["deleteEntries"])
        self.assertEqual(result["planned"]["entriesToDelete"], 1)
        self.assertEqual(result["status"], "dry-run")

    def test_main_verify_checks_existing_data_after_localhost_replacement(self):
        source = fixture()
        source[0]["lmsConfig"]["address"] = "http://localhost:8083/ncip"
        source[0]["endpoints"][0]["address"] = "http://localhost:8083/ncip"
        source[0]["catalogConfig"]["sru"]["address"] = (
            "http://localhost:8083/sru"
        )
        replacement = "https://services.example.test/gateway"
        transformed, replacement_count = replace_localhost_urls(source, replacement)
        self.seed(transformed)
        self.state.requests.clear()

        fixture_path = Path(self.temporary.name) / "verify-fixture.json"
        result_path = Path(self.temporary.name) / "verified.json"
        fixture_path.write_text(json.dumps(source))
        with patch.dict(os.environ, {"DIRECTORY_TOKEN": TOKEN}):
            status = load_directory_data.main([
                "--base-url", self.base_url,
                "--tenant", TENANT,
                "--consortium-id", CONSORTIUM_ID,
                "--fixture", str(fixture_path),
                "--result", str(result_path),
                "--replace-localhost", replacement,
                "--verify",
            ])

        self.assertEqual(status, 0)
        self.assertTrue(self.state.requests)
        self.assertEqual(
            {request["method"] for request in self.state.requests}, {"GET"}
        )
        result = json.loads(result_path.read_text())
        self.assertEqual(result["status"], "verified")
        self.assertTrue(result["verifyOnly"])
        self.assertEqual(result["localhostUrlsReplaced"], replacement_count)
        self.assertEqual(result["planned"]["reusedEntries"], 2)

    def test_main_verify_fails_for_different_entry_data_without_writes(self):
        source = fixture()
        self.seed(source)
        institution = next(
            entry
            for entry in self.state.entries.values()
            if entry["type"] == "Institution"
        )
        institution["lmsConfig"]["address"] = "https://wrong.example/ncip"
        self.state.requests.clear()

        fixture_path = Path(self.temporary.name) / "verify-mismatch-fixture.json"
        result_path = Path(self.temporary.name) / "verify-mismatch.json"
        fixture_path.write_text(json.dumps(source))
        stderr = io.StringIO()
        with (
            patch.dict(os.environ, {"DIRECTORY_TOKEN": TOKEN}),
            contextlib.redirect_stderr(stderr),
        ):
            status = load_directory_data.main([
                "--base-url", self.base_url,
                "--tenant", TENANT,
                "--consortium-id", CONSORTIUM_ID,
                "--fixture", str(fixture_path),
                "--result", str(result_path),
                "--verify",
            ])

        self.assertEqual(status, 1)
        self.assertIn("failed verification", stderr.getvalue())
        self.assertEqual(
            {request["method"] for request in self.state.requests}, {"GET"}
        )
        self.assertEqual(json.loads(result_path.read_text())["status"], "failed")

    def test_main_verify_fails_when_entities_are_missing_without_writes(self):
        fixture_path = Path(self.temporary.name) / "verify-missing-fixture.json"
        result_path = Path(self.temporary.name) / "verify-missing.json"
        fixture_path.write_text(json.dumps(fixture()))
        stderr = io.StringIO()
        with (
            patch.dict(os.environ, {"DIRECTORY_TOKEN": TOKEN}),
            contextlib.redirect_stderr(stderr),
        ):
            status = load_directory_data.main([
                "--base-url", self.base_url,
                "--tenant", TENANT,
                "--consortium-id", CONSORTIUM_ID,
                "--fixture", str(fixture_path),
                "--result", str(result_path),
                "--verify",
            ])

        self.assertEqual(status, 1)
        self.assertIn("missing from target", stderr.getvalue())
        self.assertEqual(
            {request["method"] for request in self.state.requests}, {"GET"}
        )

    def test_partial_failure_is_recorded_and_token_is_redacted(self):
        fixture_path = Path(self.temporary.name) / "fixture.json"
        result_path = Path(self.temporary.name) / "failed.json"
        fixture_path.write_text(json.dumps(fixture()))
        self.state.fail_path = "/directory/networks"
        self.state.fail_body = f"proxy accidentally echoed {TOKEN}"
        stderr = io.StringIO()
        with (
            patch.dict(os.environ, {"DIRECTORY_TOKEN": TOKEN}),
            contextlib.redirect_stderr(stderr),
        ):
            status = load_directory_data.main([
                "--base-url", self.base_url,
                "--tenant", TENANT,
                "--consortium-id", CONSORTIUM_ID,
                "--fixture", str(fixture_path),
                "--result", str(result_path),
            ])
        self.assertEqual(status, 1)
        result = result_path.read_text()
        result_data = json.loads(result)
        self.assertIn('"status": "failed"', result)
        self.assertEqual(len(result_data["created"]["tiers"]), 1)
        self.assertNotIn(TOKEN, result)
        self.assertNotIn(TOKEN, stderr.getvalue())
        for diagnostic in (result_data["error"], stderr.getvalue()):
            self.assertIn(
                f"Attempted request: POST {self.base_url}/directory/networks",
                diagnostic,
            )
            self.assertIn('"X-Okapi-Tenant": "example_tenant"', diagnostic)
            self.assertIn('"X-Okapi-Token": "[REDACTED]"', diagnostic)
            self.assertIn('Request content: {"name": "Shared network"', diagnostic)
            self.assertIn('"X-Test-Request-Id": "request-123"', diagnostic)
            self.assertIn('"Set-Cookie": "[REDACTED]"', diagnostic)


class ValidationTest(unittest.TestCase):
    def test_replace_localhost_urls_preserves_paths_and_non_local_urls(self):
        value = {
            "local": "http://localhost:8083/ncip?version=1#response",
            "nested": ["https://localhost/iso18626"],
            "external": "https://example.test/localhost",
            "text": "connect to localhost",
        }

        transformed, count = replace_localhost_urls(
            value, "https://services.example.test/gateway"
        )

        self.assertEqual(count, 2)
        self.assertEqual(
            transformed["local"],
            "https://services.example.test/gateway/ncip?version=1#response",
        )
        self.assertEqual(
            transformed["nested"][0],
            "https://services.example.test/gateway/iso18626",
        )
        self.assertEqual(transformed["external"], value["external"])
        self.assertEqual(transformed["text"], value["text"])
        self.assertEqual(value["local"], "http://localhost:8083/ncip?version=1#response")

    def test_rejects_invalid_localhost_replacement(self):
        with self.assertRaisesRegex(LoadError, "absolute HTTP"):
            normalize_localhost_replacement("services.example.test")

    def test_rejects_conflicting_catalog_definitions(self):
        value = fixture()
        value[1]["tiers"][0] = dict(value[1]["tiers"][0], cost=12.0)
        with self.assertRaisesRegex(LoadError, "conflicting definitions"):
            validate_dataset(value)

    def test_rejects_missing_branch_parent(self):
        value = fixture()
        value[1]["parent"] = str(uuid.uuid4())
        with self.assertRaisesRegex(LoadError, "does not resolve"):
            validate_dataset(value)

    def test_rejects_priority_outside_int32(self):
        value = fixture()
        value[0]["networks"][0]["priority"] = 2**31
        with self.assertRaisesRegex(LoadError, "int32"):
            validate_dataset(value)


if __name__ == "__main__":
    unittest.main()
