#!/usr/bin/env python3
"""Seed the Bruno fixture into a fresh Directory service."""

from __future__ import annotations

import argparse
import copy
import json
import os
import sys
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlsplit, urlunsplit
from urllib.request import Request, urlopen


PERMISSIONS = '["directory.consortium.all"]'
PAGE_SIZE = 1000
EXPECTED_SYMBOLS = {"ISIL:US-RS1", "ISIL:US-RS2", "ISIL:US-RS3"}
ENTRY_FIELDS = {
    "name",
    "type",
    "description",
    "organizationId",
    "contactName",
    "email",
    "fromEmail",
    "tenant",
    "vendor",
    "phoneNumber",
    "lmsLocationCode",
    "illConfig",
    "hrid",
    "timeZone",
    "symbols",
    "addresses",
    "closures",
    "endpoints",
    "lmsConfig",
    "catalogConfig",
    "holdingsPolicy",
}


class SeedError(RuntimeError):
    """A fixture validation or Directory API error."""


class ApiError(SeedError):
    def __init__(self, message: str, status: int | None = None):
        super().__init__(message)
        self.status = status


def normalize_base_url(value: str) -> str:
    parts = urlsplit(value.rstrip("/"))
    if parts.scheme not in {"http", "https"} or not parts.netloc:
        raise SeedError("DIRECTORY_BASE_URL must be an absolute http(s) URL")
    path = parts.path.rstrip("/")
    if not path.endswith("/directory"):
        path += "/directory"
    return urlunsplit((parts.scheme, parts.netloc, path, "", ""))


class DirectoryClient:
    def __init__(self, base_url: str, timeout: float = 30.0):
        self.base_url = normalize_base_url(base_url)
        self.timeout = timeout

    def request(
        self,
        method: str,
        path: str,
        payload: dict[str, Any] | None = None,
        expected: tuple[int, ...] = (200,),
    ) -> Any:
        data = None
        headers = {
            "Accept": "application/json",
            "X-Okapi-Permissions": PERMISSIONS,
        }
        if payload is not None:
            data = json.dumps(payload).encode()
            headers["Content-Type"] = "application/json"
        url = self.base_url + path
        request = Request(url, data=data, headers=headers, method=method)
        try:
            with urlopen(request, timeout=self.timeout) as response:
                body = response.read()
                if response.status not in expected:
                    raise ApiError(
                        f"{method} {url} returned HTTP {response.status}: "
                        f"{body.decode(errors='replace')}",
                        response.status,
                    )
        except HTTPError as error:
            body = error.read().decode(errors="replace")
            raise ApiError(
                f"{method} {url} returned HTTP {error.code}: {body}", error.code
            ) from error
        except URLError as error:
            raise ApiError(f"{method} {url} failed: {error.reason}") from error

        if not body:
            return None
        try:
            return json.loads(body)
        except json.JSONDecodeError as error:
            raise ApiError(f"{method} {url} returned invalid JSON") from error

    def get(self, path: str) -> Any:
        return self.request("GET", path)

    def post(self, path: str, payload: dict[str, Any]) -> Any:
        return self.request("POST", path, payload, expected=(201,))

    def list_all(self, path: str) -> list[dict[str, Any]]:
        result: list[dict[str, Any]] = []
        offset = 0
        while True:
            separator = "&" if "?" in path else "?"
            page = self.get(f"{path}{separator}limit={PAGE_SIZE}&offset={offset}")
            items = page.get("items") if isinstance(page, dict) else None
            if not isinstance(items, list):
                raise SeedError(f"GET {path} response does not contain an items array")
            result.extend(items)
            if len(items) < PAGE_SIZE:
                return result
            offset += len(items)


@dataclass(frozen=True)
class CatalogItem:
    name: str
    definition: dict[str, Any]
    source_ids: tuple[str, ...]


def require_string(value: Any, context: str) -> str:
    if not isinstance(value, str) or not value:
        raise SeedError(f"{context} must be a non-empty string")
    return value


def build_catalog(entries: list[dict[str, Any]], field: str) -> list[CatalogItem]:
    definitions: dict[str, dict[str, Any]] = {}
    source_ids: dict[str, list[str]] = {}
    source_names: dict[str, str] = {}

    for entry_index, entry in enumerate(entries):
        values = entry.get(field) or []
        if not isinstance(values, list):
            raise SeedError(f"entry {entry_index}.{field} must be an array")
        for value_index, value in enumerate(values):
            context = f"entry {entry_index}.{field}[{value_index}]"
            if not isinstance(value, dict):
                raise SeedError(f"{context} must be an object")
            name = require_string(value.get("name"), f"{context}.name")
            source_id = require_string(value.get("id"), f"{context}.id")
            definition = {
                key: copy.deepcopy(item)
                for key, item in value.items()
                if key not in {"id", "consortium", "priority"}
            }
            previous_name = source_names.get(source_id)
            if previous_name is not None and previous_name != name:
                raise SeedError(
                    f"source {field[:-1]} ID {source_id} has conflicting names"
                )
            if name in definitions and definitions[name] != definition:
                raise SeedError(f"{field[:-1]} {name!r} has conflicting definitions")
            definitions.setdefault(name, definition)
            source_names[source_id] = name
            ids = source_ids.setdefault(name, [])
            if source_id not in ids:
                ids.append(source_id)

    return [
        CatalogItem(name, definitions[name], tuple(source_ids[name]))
        for name in sorted(definitions)
    ]


def clean_entry(entry: dict[str, Any], consortium_id: str) -> dict[str, Any]:
    payload = {
        key: copy.deepcopy(value) for key, value in entry.items() if key in ENTRY_FIELDS
    }
    payload["parent"] = consortium_id

    for field in ("symbols", "endpoints"):
        for value in payload.get(field) or []:
            value.pop("id", None)
    for address in payload.get("addresses") or []:
        address.pop("id", None)
        address.pop("entry", None)
        for component in address.get("addressComponents") or []:
            component.pop("id", None)
            component.pop("address", None)
    for closure in payload.get("closures") or []:
        closure.pop("id", None)
        closure.pop("entry", None)
    ill_config = payload.get("illConfig")
    if isinstance(ill_config, dict):
        for symbol in ill_config.get("lendersOfLastResort") or []:
            symbol.pop("id", None)
    return payload


def validate_fixture(value: Any) -> list[dict[str, Any]]:
    if not isinstance(value, list) or not value:
        raise SeedError("fixture must be a non-empty array")
    seen_ids: set[str] = set()
    symbols: set[str] = set()
    for index, entry in enumerate(value):
        if not isinstance(entry, dict):
            raise SeedError(f"entry {index} must be an object")
        source_id = require_string(entry.get("id"), f"entry {index}.id")
        if source_id in seen_ids:
            raise SeedError(f"duplicate entry source ID {source_id}")
        seen_ids.add(source_id)
        require_string(entry.get("name"), f"entry {index}.name")
        if entry.get("type") != "Institution":
            raise SeedError(f"entry {source_id} must have type Institution")
        for symbol in entry.get("symbols") or []:
            authority = require_string(symbol.get("authority"), "symbol.authority")
            code = require_string(symbol.get("symbol"), "symbol.symbol")
            symbols.add(f"{authority}:{code}")
        for network in entry.get("networks") or []:
            priority = network.get("priority", 0)
            if not isinstance(priority, int) or isinstance(priority, bool):
                raise SeedError(f"entry {source_id} network priority must be an integer")
    if symbols != EXPECTED_SYMBOLS:
        raise SeedError(
            f"fixture symbols must be {sorted(EXPECTED_SYMBOLS)}, got {sorted(symbols)}"
        )
    build_catalog(value, "tiers")
    build_catalog(value, "networks")
    return value


class Seeder:
    def __init__(self, client: DirectoryClient, entries: list[dict[str, Any]], consortium_name: str):
        self.client = client
        self.entries = validate_fixture(entries)
        self.consortium_name = require_string(consortium_name, "consortium name")
        self.entry_ids: dict[str, str] = {}
        self.tier_ids: dict[str, str] = {}
        self.network_ids: dict[str, str] = {}

    def wait_until_ready(self, attempts: int, interval: float) -> None:
        last_error: ApiError | None = None
        for attempt in range(1, attempts + 1):
            try:
                self.client.get("/entries?limit=1")
                print("Directory is ready")
                return
            except ApiError as error:
                last_error = error
                if error.status is not None and error.status < 500:
                    raise
                if attempt < attempts:
                    time.sleep(interval)
        raise SeedError(f"Directory did not become ready after {attempts} attempts: {last_error}")

    def ensure_empty(self) -> None:
        for path in ("/entries", "/tiers", "/networks"):
            if self.client.list_all(path):
                raise SeedError(f"Directory {path} is not empty; recreate the test volume")

    @staticmethod
    def created_id(response: Any, context: str) -> str:
        value = response.get("id") if isinstance(response, dict) else None
        return require_string(value, f"{context} response.id")

    def create_catalog(
        self, path: str, items: list[CatalogItem], consortium_id: str
    ) -> dict[str, str]:
        mapping: dict[str, str] = {}
        for item in items:
            payload = copy.deepcopy(item.definition)
            payload["consortium"] = consortium_id
            created = self.created_id(self.client.post(path, payload), f"POST {path}")
            for source_id in item.source_ids:
                mapping[source_id] = created
        return mapping

    def seed(self) -> None:
        self.ensure_empty()
        consortium_id = self.created_id(
            self.client.post(
                "/entries", {"name": self.consortium_name, "type": "Consortium"}
            ),
            "POST /entries",
        )
        self.tier_ids = self.create_catalog(
            "/tiers", build_catalog(self.entries, "tiers"), consortium_id
        )
        self.network_ids = self.create_catalog(
            "/networks", build_catalog(self.entries, "networks"), consortium_id
        )

        for entry in self.entries:
            source_id = entry["id"]
            self.entry_ids[source_id] = self.created_id(
                self.client.post("/entries", clean_entry(entry, consortium_id)),
                "POST /entries",
            )

        for entry in self.entries:
            created_entry = self.entry_ids[entry["id"]]
            entry_path = f"/entries/by-id/{quote(created_entry, safe='')}"
            for tier in entry.get("tiers") or []:
                created_tier = self.tier_ids[tier["id"]]
                self.client.post(f"{entry_path}/tiers", {"id": created_tier})
            for network in entry.get("networks") or []:
                created_network = self.network_ids[network["id"]]
                self.client.post(
                    f"{entry_path}/networks",
                    {"id": created_network, "priority": network.get("priority", 0)},
                )

    def verify(self) -> None:
        entries = self.client.list_all("/entries")
        institutions = [entry for entry in entries if entry.get("type") == "Institution"]
        consortiums = [entry for entry in entries if entry.get("type") == "Consortium"]
        if len(institutions) != len(self.entries) or len(consortiums) != 1:
            raise SeedError(
                f"expected {len(self.entries)} institutions and one consortium, "
                f"got {len(institutions)} and {len(consortiums)}"
            )
        if len(self.client.list_all("/networks")) != 1:
            raise SeedError("expected exactly one network")
        if len(self.client.list_all("/tiers")) != 2:
            raise SeedError("expected exactly two tiers")

        source_by_symbol = {
            f"{symbol['authority']}:{symbol['symbol']}": entry
            for entry in self.entries
            for symbol in entry.get("symbols") or []
        }
        for symbol in sorted(EXPECTED_SYMBOLS):
            entry = self.client.get(f"/entries/by-symbol/{quote(symbol, safe='')}")
            entry_id = require_string(entry.get("id"), f"entry {symbol}.id")
            expected = source_by_symbol[symbol]
            tiers = self.client.list_all(f"/entries/by-id/{entry_id}/tiers")
            networks = self.client.list_all(f"/entries/by-id/{entry_id}/networks")
            expected_tiers = {self.tier_ids[item["id"]] for item in expected.get("tiers") or []}
            actual_tiers = {item.get("id") for item in tiers}
            if actual_tiers != expected_tiers:
                raise SeedError(f"entry {symbol} has incorrect tier memberships")
            if len(networks) != 1:
                raise SeedError(f"entry {symbol} must have exactly one network membership")
            expected_network = expected["networks"][0]
            if networks[0].get("id") != self.network_ids[expected_network["id"]]:
                raise SeedError(f"entry {symbol} has an incorrect network membership")
            if networks[0].get("priority") != expected_network.get("priority", 0):
                raise SeedError(f"entry {symbol} has an incorrect network priority")
        print("Directory seed verification succeeded")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--base-url", default=os.getenv("DIRECTORY_BASE_URL", "http://directory:8086")
    )
    parser.add_argument(
        "--fixture", default=os.getenv("DIRECTORY_FIXTURE", "/data/directory.json")
    )
    parser.add_argument(
        "--consortium-name",
        default=os.getenv("DIRECTORY_CONSORTIUM_NAME", "Bruno Test Consortium"),
    )
    parser.add_argument(
        "--wait-attempts", type=int, default=int(os.getenv("DIRECTORY_WAIT_ATTEMPTS", "60"))
    )
    parser.add_argument(
        "--wait-interval",
        type=float,
        default=float(os.getenv("DIRECTORY_WAIT_INTERVAL_SECONDS", "2")),
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.wait_attempts < 1 or args.wait_interval < 0:
        raise SeedError("retry settings must be non-negative, with at least one attempt")
    try:
        fixture = json.loads(Path(args.fixture).read_text())
        seeder = Seeder(DirectoryClient(args.base_url), fixture, args.consortium_name)
        seeder.wait_until_ready(args.wait_attempts, args.wait_interval)
        seeder.seed()
        seeder.verify()
        return 0
    except (OSError, json.JSONDecodeError, SeedError) as error:
        print(f"Directory seed failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
