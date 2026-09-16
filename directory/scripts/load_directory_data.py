#!/usr/bin/env python3
"""Load an exported Directory dataset through an Okapi-routed Directory API."""

from __future__ import annotations

import argparse
import copy
import json
import os
import sys
import tempfile
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlsplit, urlunsplit
from urllib.request import Request, urlopen
from uuid import UUID


PAGE_SIZE = 1000
INT32_MIN = -(2**31)
INT32_MAX = 2**31 - 1
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
PIECEMEAL_ENTRY_FIELDS = (
    "symbols",
    "lmsConfig",
    "endpoints",
    "holdingsPolicy",
    "catalogConfig",
)


class LoadError(RuntimeError):
    """A dataset, API, or verification error."""


class ApiError(LoadError):
    def __init__(self, message: str, status: int | None = None):
        super().__init__(message)
        self.status = status


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def require_string(value: Any, context: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise LoadError(f"{context} must be a non-empty string")
    return value


def require_uuid(value: Any, context: str) -> str:
    text = require_string(value, context)
    try:
        UUID(text)
    except ValueError as error:
        raise LoadError(f"{context} must be a UUID") from error
    return text


def normalize_base_url(value: str) -> str:
    parts = urlsplit(value.rstrip("/"))
    if parts.scheme not in {"http", "https"} or not parts.netloc:
        raise LoadError("--base-url must be an absolute HTTP(S) URL")
    if parts.query or parts.fragment:
        raise LoadError("--base-url must not contain a query string or fragment")
    path = parts.path.rstrip("/")
    if not path.endswith("/directory"):
        path += "/directory"
    return urlunsplit((parts.scheme, parts.netloc, path, "", ""))


def normalize_localhost_replacement(value: str) -> str:
    parts = urlsplit(value.rstrip("/"))
    if parts.scheme not in {"http", "https"} or not parts.netloc:
        raise LoadError("--replace-localhost must be an absolute HTTP(S) URL")
    if parts.username is not None or parts.password is not None:
        raise LoadError("--replace-localhost must not contain credentials")
    if parts.query or parts.fragment:
        raise LoadError(
            "--replace-localhost must not contain a query string or fragment"
        )
    return urlunsplit((parts.scheme, parts.netloc, parts.path.rstrip("/"), "", ""))


def replace_localhost_urls(value: Any, replacement: str) -> tuple[Any, int]:
    replacement_parts = urlsplit(normalize_localhost_replacement(replacement))

    def transform(item: Any) -> tuple[Any, int]:
        if isinstance(item, dict):
            result: dict[str, Any] = {}
            count = 0
            for key, child in item.items():
                transformed, child_count = transform(child)
                result[key] = transformed
                count += child_count
            return result, count
        if isinstance(item, list):
            result_list: list[Any] = []
            count = 0
            for child in item:
                transformed, child_count = transform(child)
                result_list.append(transformed)
                count += child_count
            return result_list, count
        if not isinstance(item, str):
            return item, 0

        original = urlsplit(item)
        if original.scheme not in {"http", "https"} or original.hostname != "localhost":
            return item, 0
        prefix = replacement_parts.path.rstrip("/")
        suffix = original.path
        if prefix and suffix:
            path = prefix + "/" + suffix.lstrip("/")
        else:
            path = prefix or suffix
        transformed = urlunsplit(
            (
                replacement_parts.scheme,
                replacement_parts.netloc,
                path,
                original.query,
                original.fragment,
            )
        )
        return transformed, 1

    return transform(value)


def contains_expected(actual: Any, expected: Any) -> bool:
    """Return whether actual recursively contains all values from expected."""
    if isinstance(expected, dict):
        return isinstance(actual, dict) and all(
            key in actual and contains_expected(actual[key], value)
            for key, value in expected.items()
        )
    if isinstance(expected, list):
        if not isinstance(actual, list):
            return False
        unused = list(actual)
        for expected_item in expected:
            for index, actual_item in enumerate(unused):
                if contains_expected(actual_item, expected_item):
                    unused.pop(index)
                    break
            else:
                return False
        return True
    return actual == expected


class DirectoryClient:
    def __init__(
        self, base_url: str, token: str, tenant: str, timeout: float = 30.0
    ):
        self.base_url = normalize_base_url(base_url)
        self.token = require_string(token, "DIRECTORY_TOKEN")
        self.tenant = require_string(tenant, "--tenant")
        if timeout <= 0:
            raise LoadError("--timeout must be greater than zero")
        self.timeout = timeout

    @staticmethod
    def request_details(
        method: str,
        url: str,
        headers: dict[str, str],
        data: bytes | None,
    ) -> str:
        safe_headers = dict(headers)
        if "X-Okapi-Token" in safe_headers:
            safe_headers["X-Okapi-Token"] = "[REDACTED]"
        content = data.decode("utf-8", errors="replace") if data else "<none>"
        return (
            f"Attempted request: {method} {url}\n"
            f"Request headers: {json.dumps(safe_headers, sort_keys=True)}\n"
            f"Request content: {content}"
        )

    def response_header_details(self, headers: Any) -> str:
        if headers is None:
            return "<none>"
        safe_headers: dict[str, str | list[str]] = {}
        sensitive = {"authorization", "proxy-authorization", "set-cookie", "x-okapi-token"}
        for name, value in headers.items():
            safe_value = "[REDACTED]" if name.lower() in sensitive else value
            safe_value = safe_value.replace(self.token, "[REDACTED]")
            previous = safe_headers.get(name)
            if previous is None:
                safe_headers[name] = safe_value
            elif isinstance(previous, list):
                previous.append(safe_value)
            else:
                safe_headers[name] = [previous, safe_value]
        return json.dumps(safe_headers, sort_keys=True)

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
            "X-Okapi-Token": self.token,
            "X-Okapi-Tenant": self.tenant,
        }
        if payload is not None:
            data = json.dumps(payload).encode("utf-8")
            headers["Content-Type"] = "application/json"

        url = self.base_url + path
        request = Request(url, data=data, headers=headers, method=method)
        request_details = self.request_details(method, url, headers, data)
        response_headers = "<none>"
        try:
            with urlopen(request, timeout=self.timeout) as response:
                body = response.read()
                response_headers = self.response_header_details(response.headers)
                if response.status not in expected:
                    raise ApiError(
                        f"{method} {url} returned HTTP {response.status}: "
                        f"{body.decode(errors='replace')}\n"
                        f"Response headers: {response_headers}\n{request_details}",
                        response.status,
                    )
        except HTTPError as error:
            body = error.read().decode(errors="replace")
            response_headers = self.response_header_details(error.headers)
            raise ApiError(
                f"{method} {url} returned HTTP {error.code}: {body}\n"
                f"Response headers: {response_headers}\n{request_details}",
                error.code,
            ) from error
        except URLError as error:
            raise ApiError(
                f"{method} {url} failed: {error.reason}\n"
                f"Response headers: <none>\n{request_details}"
            ) from error

        if not body:
            return None
        try:
            return json.loads(body)
        except json.JSONDecodeError as error:
            raise ApiError(
                f"{method} {url} returned invalid JSON\n"
                f"Response headers: {response_headers}\n{request_details}"
            ) from error

    def get(self, path: str) -> Any:
        return self.request("GET", path)

    def post(self, path: str, payload: dict[str, Any]) -> Any:
        return self.request("POST", path, payload, expected=(201,))

    def patch(self, path: str, payload: dict[str, Any]) -> None:
        self.request("PATCH", path, payload, expected=(204,))

    def delete(self, path: str) -> None:
        self.request("DELETE", path, expected=(204,))

    def list_all(self, path: str) -> list[dict[str, Any]]:
        result: list[dict[str, Any]] = []
        offset = 0
        while True:
            separator = "&" if "?" in path else "?"
            page = self.get(
                f"{path}{separator}limit={PAGE_SIZE}&offset={offset}"
            )
            items = page.get("items") if isinstance(page, dict) else None
            if not isinstance(items, list) or not all(
                isinstance(item, dict) for item in items
            ):
                raise ApiError(f"GET {path} response does not contain an items array")
            result.extend(items)
            if len(items) < PAGE_SIZE:
                return result
            offset += len(items)


@dataclass(frozen=True)
class CatalogItem:
    name: str
    definition: dict[str, Any]
    source_ids: tuple[str, ...]


@dataclass(frozen=True)
class ValidatedDataset:
    entries: tuple[dict[str, Any], ...]
    tiers: tuple[CatalogItem, ...]
    networks: tuple[CatalogItem, ...]


def build_catalog(entries: list[dict[str, Any]], field: str) -> list[CatalogItem]:
    definitions: dict[str, dict[str, Any]] = {}
    source_ids: dict[str, list[str]] = {}
    source_names: dict[str, str] = {}

    for entry_index, entry in enumerate(entries):
        values = entry.get(field) or []
        if not isinstance(values, list):
            raise LoadError(f"entry {entry_index}.{field} must be an array")
        membership_names: set[str] = set()
        for value_index, value in enumerate(values):
            context = f"entry {entry_index}.{field}[{value_index}]"
            if not isinstance(value, dict):
                raise LoadError(f"{context} must be an object")
            name = require_string(value.get("name"), f"{context}.name")
            source_id = require_uuid(value.get("id"), f"{context}.id")
            if name in membership_names:
                raise LoadError(
                    f"entry {entry_index} contains duplicate {field[:-1]} {name!r}"
                )
            membership_names.add(name)

            definition = {
                key: copy.deepcopy(item)
                for key, item in value.items()
                if key not in {"id", "consortium", "priority"}
            }
            previous_name = source_names.get(source_id)
            if previous_name is not None and previous_name != name:
                raise LoadError(
                    f"source {field[:-1]} ID {source_id} has conflicting names"
                )
            if name in definitions and definitions[name] != definition:
                raise LoadError(f"{field[:-1]} {name!r} has conflicting definitions")
            definitions.setdefault(name, definition)
            source_names[source_id] = name
            ids = source_ids.setdefault(name, [])
            if source_id not in ids:
                ids.append(source_id)

            if field == "networks":
                priority = value.get("priority", 0)
                if (
                    not isinstance(priority, int)
                    or isinstance(priority, bool)
                    or not INT32_MIN <= priority <= INT32_MAX
                ):
                    raise LoadError(f"{context}.priority must be an int32 integer")

    return [
        CatalogItem(name, definitions[name], tuple(source_ids[name]))
        for name in sorted(definitions)
    ]


def validate_dataset(value: Any) -> ValidatedDataset:
    if not isinstance(value, list) or not value:
        raise LoadError("fixture must be a non-empty array")

    entries: list[dict[str, Any]] = []
    by_id: dict[str, dict[str, Any]] = {}
    names: set[str] = set()
    symbols: set[str] = set()

    for index, entry in enumerate(value):
        if not isinstance(entry, dict):
            raise LoadError(f"entry {index} must be an object")
        source_id = require_uuid(entry.get("id"), f"entry {index}.id")
        if source_id in by_id:
            raise LoadError(f"duplicate entry source ID {source_id}")
        name = require_string(entry.get("name"), f"entry {index}.name")
        if name in names:
            raise LoadError(f"duplicate entry name {name!r}")
        names.add(name)

        entry_type = entry.get("type")
        if entry_type not in {"Institution", "Branch"}:
            raise LoadError(
                f"entry {source_id} must have type Institution or Branch"
            )
        entry_symbols = entry.get("symbols") or []
        if not isinstance(entry_symbols, list):
            raise LoadError(f"entry {source_id}.symbols must be an array")
        for symbol_index, symbol in enumerate(entry_symbols):
            if not isinstance(symbol, dict):
                raise LoadError(
                    f"entry {source_id}.symbols[{symbol_index}] must be an object"
                )
            authority = require_string(
                symbol.get("authority"),
                f"entry {source_id}.symbols[{symbol_index}].authority",
            ).upper()
            code = require_string(
                symbol.get("symbol"),
                f"entry {source_id}.symbols[{symbol_index}].symbol",
            ).upper()
            combined = f"{authority}:{code}"
            if combined in symbols:
                raise LoadError(f"duplicate entry symbol {combined}")
            symbols.add(combined)

        entries.append(copy.deepcopy(entry))
        by_id[source_id] = entry

    for entry in entries:
        if entry["type"] != "Branch":
            continue
        source_id = entry["id"]
        parent_id = require_uuid(entry.get("parent"), f"entry {source_id}.parent")
        parent = by_id.get(parent_id)
        if parent is None:
            raise LoadError(f"entry {source_id}.parent does not resolve in the fixture")
        if parent.get("type") != "Institution":
            raise LoadError(f"entry {source_id}.parent must reference an Institution")

    tiers = build_catalog(entries, "tiers")
    networks = build_catalog(entries, "networks")
    return ValidatedDataset(tuple(entries), tuple(tiers), tuple(networks))


def clean_entry(entry: dict[str, Any], parent_id: str) -> dict[str, Any]:
    payload = {
        key: copy.deepcopy(value) for key, value in entry.items() if key in ENTRY_FIELDS
    }
    payload["parent"] = parent_id

    for field in ("symbols", "endpoints"):
        for value in payload.get(field) or []:
            value.pop("id", None)
            value.pop("entry", None)
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
            symbol.pop("entry", None)
    return payload


class ResultManifest:
    def __init__(
        self,
        path: Path,
        base_url: str,
        tenant: str,
        consortium_id: str,
        fixture: Path,
        dry_run: bool,
        allow_existing: bool = False,
        piecemeal: bool = False,
        localhost_replacement: str | None = None,
        delete_entries: bool = False,
        verify_only: bool = False,
    ):
        self.path = path
        self.data: dict[str, Any] = {
            "schemaVersion": 1,
            "startedAt": utc_now(),
            "finishedAt": None,
            "status": "running",
            "phase": "initializing",
            "baseUrl": base_url,
            "tenant": tenant,
            "consortiumId": consortium_id,
            "fixture": str(fixture.resolve()),
            "dryRun": dry_run,
            "allowExisting": allow_existing,
            "piecemeal": piecemeal,
            "deleteEntries": delete_entries,
            "verifyOnly": verify_only,
            "localhostReplacement": localhost_replacement,
            "localhostUrlsReplaced": 0,
            "maps": {"entries": {}, "tiers": {}, "networks": {}},
            "created": {
                "entries": [],
                "tiers": [],
                "networks": [],
                "entryTiers": [],
                "entryNetworks": [],
            },
            "reused": {
                "entries": [],
                "tiers": [],
                "networks": [],
                "entryTiers": [],
                "entryNetworks": [],
            },
            "patchedEntries": [],
            "deletedEntries": [],
        }

    def write(self) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        temporary_name: str | None = None
        try:
            with tempfile.NamedTemporaryFile(
                "w",
                encoding="utf-8",
                dir=self.path.parent,
                prefix=f".{self.path.name}.",
                delete=False,
            ) as temporary:
                temporary_name = temporary.name
                json.dump(self.data, temporary, indent=2, sort_keys=True)
                temporary.write("\n")
            os.replace(temporary_name, self.path)
        finally:
            if temporary_name is not None:
                try:
                    Path(temporary_name).unlink(missing_ok=True)
                except OSError:
                    pass

    def phase(self, name: str) -> None:
        self.data["phase"] = name
        self.write()

    def map_id(self, category: str, source_ids: tuple[str, ...], created_id: str) -> None:
        mapping = self.data["maps"][category]
        for source_id in source_ids:
            mapping[source_id] = created_id
        self.write()

    def record_created(self, category: str, value: dict[str, Any]) -> None:
        self.data["created"][category].append(value)
        self.write()

    def record_reused(self, category: str, value: dict[str, Any]) -> None:
        self.data["reused"][category].append(value)
        self.write()

    def record_patch(self, entry_id: str, field: str) -> None:
        self.data["patchedEntries"].append({"entry": entry_id, "field": field})
        self.write()

    def record_deleted(self, value: dict[str, Any]) -> None:
        self.data["deletedEntries"].append(value)
        self.write()

    def finish(self, status: str, error: str | None = None) -> None:
        self.data["status"] = status
        self.data["finishedAt"] = utc_now()
        if error is not None:
            self.data["error"] = error
        self.write()


class DirectoryLoader:
    def __init__(
        self,
        client: DirectoryClient,
        dataset: ValidatedDataset,
        consortium_id: str,
        manifest: ResultManifest,
        allow_existing: bool = False,
        piecemeal: bool = False,
        delete_entries: bool = False,
        verify_only: bool = False,
    ):
        self.client = client
        self.dataset = dataset
        self.consortium_id = require_uuid(consortium_id, "--consortium-id")
        self.manifest = manifest
        self.allow_existing = allow_existing
        self.piecemeal = piecemeal
        self.delete_entries = delete_entries
        self.verify_only = verify_only
        self.entry_ids: dict[str, str] = {}
        self.tier_ids: dict[str, str] = {}
        self.network_ids: dict[str, str] = {}
        self.reused_entry_source_ids: set[str] = set()
        self.existing_entry_tiers: dict[str, set[str]] = {}
        self.existing_entry_networks: dict[str, dict[str, int]] = {}
        self.entries_to_delete: list[tuple[dict[str, Any], str]] = []

    @staticmethod
    def created_id(response: Any, context: str) -> str:
        value = response.get("id") if isinstance(response, dict) else None
        return require_uuid(value, f"{context} response.id")

    def preflight(self) -> dict[str, int]:
        self.manifest.phase("preflight")
        consortium = self.client.get(
            f"/entries/by-id/{quote(self.consortium_id, safe='')}"
        )
        if not isinstance(consortium, dict) or consortium.get("type") != "Consortium":
            raise LoadError(
                f"entry {self.consortium_id} is not a Consortium entry"
            )

        existing_entries = self.client.list_all("/entries")
        existing_tiers = self.client.list_all(
            f"/entries/by-id/{quote(self.consortium_id, safe='')}/tiers"
        )
        existing_networks = self.client.list_all(
            f"/entries/by-id/{quote(self.consortium_id, safe='')}/networks"
        )

        target_entry_ids = {self.consortium_id}
        while True:
            children = {
                entry.get("id")
                for entry in existing_entries
                if entry.get("parent") in target_entry_ids
                and isinstance(entry.get("id"), str)
            }
            expanded = target_entry_ids | children
            if expanded == target_entry_ids:
                break
            target_entry_ids = expanded

        collisions: list[str] = []
        matched_entries = self._resolve_existing_entries(
            existing_entries, target_entry_ids, collisions
        )
        reused_entries = [] if self.delete_entries else matched_entries
        self.entries_to_delete = matched_entries if self.delete_entries else []
        if self.delete_entries:
            self._validate_deletions(existing_entries)

        reused_tiers = self._resolve_existing_catalog(
            self.dataset.tiers, existing_tiers, "tier", collisions
        )
        reused_networks = self._resolve_existing_catalog(
            self.dataset.networks, existing_networks, "network", collisions
        )

        if collisions:
            details = ", ".join(sorted(set(collisions)))
            raise LoadError(f"target collisions detected: {details}")
        if self.verify_only:
            self._require_all_entities_present(
                matched_entries, reused_tiers, reused_networks
            )

        self._record_reused_catalog("tiers", reused_tiers, self.tier_ids)
        self._record_reused_catalog("networks", reused_networks, self.network_ids)
        self._record_reused_entries(reused_entries)
        self._read_existing_memberships()

        return {
            "entries": len(self.dataset.entries),
            "tiers": len(self.dataset.tiers),
            "networks": len(self.dataset.networks),
            "entriesToDelete": len(self.entries_to_delete),
            "reusedEntries": len(reused_entries),
            "reusedTiers": len(reused_tiers),
            "reusedNetworks": len(reused_networks),
            "tierMemberships": sum(
                len(entry.get("tiers") or []) for entry in self.dataset.entries
            ),
            "networkMemberships": sum(
                len(entry.get("networks") or []) for entry in self.dataset.entries
            ),
        }

    def _require_all_entities_present(
        self,
        matched_entries: list[tuple[dict[str, Any], str]],
        matched_tiers: list[tuple[CatalogItem, str]],
        matched_networks: list[tuple[CatalogItem, str]],
    ) -> None:
        matched_entry_ids = {entry["id"] for entry, _ in matched_entries}
        matched_tier_names = {item.name for item, _ in matched_tiers}
        matched_network_names = {item.name for item, _ in matched_networks}
        missing = [
            f"entry {entry['name']!r}"
            for entry in self.dataset.entries
            if entry["id"] not in matched_entry_ids
        ]
        missing.extend(
            f"tier {item.name!r}"
            for item in self.dataset.tiers
            if item.name not in matched_tier_names
        )
        missing.extend(
            f"network {item.name!r}"
            for item in self.dataset.networks
            if item.name not in matched_network_names
        )
        if missing:
            raise LoadError(
                "verification failed; missing from target: " + ", ".join(missing)
            )

    @staticmethod
    def _entry_symbols(entry: dict[str, Any]) -> set[str]:
        result: set[str] = set()
        for symbol in entry.get("symbols") or []:
            if not isinstance(symbol, dict):
                continue
            authority = symbol.get("authority")
            code = symbol.get("symbol")
            if isinstance(authority, str) and isinstance(code, str):
                result.add(f"{authority.upper()}:{code.upper()}")
        return result

    def _resolve_existing_entries(
        self,
        existing_entries: list[dict[str, Any]],
        target_entry_ids: set[str],
        collisions: list[str],
    ) -> list[tuple[dict[str, Any], str]]:
        matched: list[tuple[dict[str, Any], str]] = []
        if self.delete_entries:
            action = "delete"
        elif self.verify_only:
            action = "verify"
        else:
            action = "reuse"
        resolved: dict[str, str] = {}
        ordered_source = sorted(
            self.dataset.entries,
            key=lambda entry: 0 if entry["type"] == "Institution" else 1,
        )

        for source in ordered_source:
            source_symbols = self._entry_symbols(source)
            candidates: dict[str, dict[str, Any]] = {}
            for existing in existing_entries:
                existing_id = existing.get("id")
                if not isinstance(existing_id, str):
                    continue
                name_match = (
                    existing_id in target_entry_ids
                    and existing.get("name") == source["name"]
                )
                symbol_matches = source_symbols & self._entry_symbols(existing)
                if not name_match and not symbol_matches:
                    continue
                if not (
                    self.allow_existing or self.delete_entries or self.verify_only
                ):
                    if name_match:
                        collisions.append(f"entry name {source['name']!r}")
                    for symbol in symbol_matches:
                        collisions.append(f"entry symbol {symbol}")
                    continue
                if existing_id not in target_entry_ids:
                    symbols = ", ".join(sorted(symbol_matches))
                    raise LoadError(
                        f"cannot {action} entry {source['name']!r}: symbol {symbols} "
                        "belongs to an entry outside the target consortium"
                    )
                candidates[existing_id] = existing

            if not (
                self.allow_existing or self.delete_entries or self.verify_only
            ) or not candidates:
                continue
            if len(candidates) > 1:
                raise LoadError(
                    f"cannot {action} ambiguous entry {source['name']!r}: "
                    f"found {len(candidates)} name or symbol matches"
                )
            existing_id, existing = next(iter(candidates.items()))
            existing_id = require_uuid(
                existing_id, f"existing entry {source['name']!r}.id"
            )
            if existing.get("type") != source["type"]:
                raise LoadError(
                    f"cannot {action} entry {source['name']!r}: expected type "
                    f"{source['type']}, got {existing.get('type')}"
                )
            if self.delete_entries:
                matched.append((source, existing_id))
                continue
            if source["type"] == "Institution":
                expected_parent = self.consortium_id
            else:
                expected_parent = resolved.get(source["parent"])
                if expected_parent is None:
                    raise LoadError(
                        f"cannot {action} Branch {source['name']!r} because its "
                        "source Institution parent is not also matched"
                    )
            if existing.get("parent") != expected_parent:
                raise LoadError(
                    f"cannot {action} entry {source['name']!r}: parent does not match"
                )
            resolved[source["id"]] = existing_id
            matched.append((source, existing_id))
        return matched

    def _validate_deletions(self, existing_entries: list[dict[str, Any]]) -> None:
        delete_ids = {existing_id for _, existing_id in self.entries_to_delete}
        protected_children = [
            entry
            for entry in existing_entries
            if entry.get("parent") in delete_ids and entry.get("id") not in delete_ids
        ]
        if protected_children:
            names = ", ".join(
                sorted(
                    str(entry.get("name") or entry.get("id"))
                    for entry in protected_children
                )
            )
            raise LoadError(
                "cannot delete duplicate entries because they have non-duplicate "
                f"children: {names}"
            )

    def _record_reused_entries(
        self, reused: list[tuple[dict[str, Any], str]]
    ) -> None:
        for entry, existing_id in reused:
            source_id = entry["id"]
            self.entry_ids[source_id] = existing_id
            self.reused_entry_source_ids.add(source_id)
            self.manifest.record_reused(
                "entries",
                {
                    "id": existing_id,
                    "sourceId": source_id,
                    "name": entry["name"],
                    "type": entry["type"],
                },
            )
            self.manifest.map_id("entries", (source_id,), existing_id)

    def _read_existing_memberships(self) -> None:
        for source_id in self.reused_entry_source_ids:
            entry_id = self.entry_ids[source_id]
            entry_path = f"/entries/by-id/{quote(entry_id, safe='')}"
            tiers = self.client.list_all(f"{entry_path}/tiers")
            networks = self.client.list_all(f"{entry_path}/networks")
            self.existing_entry_tiers[entry_id] = {
                item["id"] for item in tiers if isinstance(item.get("id"), str)
            }
            self.existing_entry_networks[entry_id] = {
                item["id"]: item.get("priority", 0)
                for item in networks
                if isinstance(item.get("id"), str)
            }

            source = next(
                item for item in self.dataset.entries if item["id"] == source_id
            )
            for network in source.get("networks") or []:
                network_id = self.network_ids.get(network["id"])
                if network_id is None or network_id not in self.existing_entry_networks[entry_id]:
                    continue
                actual_priority = self.existing_entry_networks[entry_id][network_id]
                expected_priority = network.get("priority", 0)
                if actual_priority != expected_priority:
                    action = "verify" if self.verify_only else "reuse"
                    raise LoadError(
                        f"cannot {action} network membership for entry "
                        f"{source['name']!r}: "
                        f"priority is {actual_priority}, expected {expected_priority}"
                    )

    def _resolve_existing_catalog(
        self,
        imported: tuple[CatalogItem, ...],
        existing: list[dict[str, Any]],
        resource: str,
        collisions: list[str],
    ) -> list[tuple[CatalogItem, str]]:
        existing_by_name: dict[str, list[dict[str, Any]]] = {}
        for item in existing:
            name = item.get("name")
            if isinstance(name, str):
                existing_by_name.setdefault(name, []).append(item)

        reused: list[tuple[CatalogItem, str]] = []
        for item in imported:
            matches = existing_by_name.get(item.name, [])
            if not matches:
                continue
            if not self.allow_existing and not self.verify_only:
                collisions.append(f"{resource} name {item.name!r}")
                continue
            if len(matches) > 1:
                raise LoadError(
                    f"cannot {'verify' if self.verify_only else 'reuse'} ambiguous "
                    f"{resource} name {item.name!r}: "
                    f"found {len(matches)} matches"
                )
            existing_id = require_uuid(
                matches[0].get("id"), f"existing {resource} {item.name!r}.id"
            )
            reused.append((item, existing_id))
        return reused

    def _record_reused_catalog(
        self,
        category: str,
        reused: list[tuple[CatalogItem, str]],
        mapping: dict[str, str],
    ) -> None:
        for item, existing_id in reused:
            for source_id in item.source_ids:
                mapping[source_id] = existing_id
            details = {
                "id": existing_id,
                "name": item.name,
                "sourceIds": list(item.source_ids),
            }
            self.manifest.record_reused(category, details)
            self.manifest.map_id(category, item.source_ids, existing_id)

    def create_catalog(
        self, path: str, category: str, items: tuple[CatalogItem, ...]
    ) -> dict[str, str]:
        mapping = self.tier_ids if category == "tiers" else self.network_ids
        for item in items:
            if all(source_id in mapping for source_id in item.source_ids):
                continue
            payload = copy.deepcopy(item.definition)
            payload["consortium"] = self.consortium_id
            created_id = self.created_id(
                self.client.post(path, payload), f"POST {path}"
            )
            for source_id in item.source_ids:
                mapping[source_id] = created_id
            self.manifest.record_created(
                category,
                {"id": created_id, "name": item.name, "sourceIds": list(item.source_ids)},
            )
            self.manifest.map_id(category, item.source_ids, created_id)
        return mapping

    def load(self) -> None:
        if self.verify_only:
            raise LoadError("verification mode cannot create or delete resources")
        self._delete_duplicate_entries()
        self.manifest.phase("creating-tiers")
        self.tier_ids = self.create_catalog(
            "/tiers", "tiers", self.dataset.tiers
        )
        self.manifest.phase("creating-networks")
        self.network_ids = self.create_catalog(
            "/networks", "networks", self.dataset.networks
        )

        self.manifest.phase("creating-institutions")
        for entry in self.dataset.entries:
            if entry["type"] == "Institution":
                self._create_entry(entry, self.consortium_id)

        self.manifest.phase("creating-branches")
        for entry in self.dataset.entries:
            if entry["type"] == "Branch":
                self._create_entry(entry, self.entry_ids[entry["parent"]])

        self.manifest.phase("creating-memberships")
        for entry in self.dataset.entries:
            created_entry_id = self.entry_ids[entry["id"]]
            entry_path = f"/entries/by-id/{quote(created_entry_id, safe='')}"
            for tier in entry.get("tiers") or []:
                created_tier_id = self.tier_ids[tier["id"]]
                if created_tier_id in self.existing_entry_tiers.get(
                    created_entry_id, set()
                ):
                    self.manifest.record_reused(
                        "entryTiers",
                        {"entry": created_entry_id, "tier": created_tier_id},
                    )
                    continue
                membership_id = self.created_id(
                    self.client.post(f"{entry_path}/tiers", {"id": created_tier_id}),
                    f"POST {entry_path}/tiers",
                )
                self.manifest.record_created(
                    "entryTiers",
                    {
                        "id": membership_id,
                        "entry": created_entry_id,
                        "tier": created_tier_id,
                    },
                )
            for network in entry.get("networks") or []:
                created_network_id = self.network_ids[network["id"]]
                priority = network.get("priority", 0)
                if created_network_id in self.existing_entry_networks.get(
                    created_entry_id, {}
                ):
                    self.manifest.record_reused(
                        "entryNetworks",
                        {
                            "entry": created_entry_id,
                            "network": created_network_id,
                            "priority": priority,
                        },
                    )
                    continue
                membership_id = self.created_id(
                    self.client.post(
                        f"{entry_path}/networks",
                        {"id": created_network_id, "priority": priority},
                    ),
                    f"POST {entry_path}/networks",
                )
                self.manifest.record_created(
                    "entryNetworks",
                    {
                        "id": membership_id,
                        "entry": created_entry_id,
                        "network": created_network_id,
                        "priority": priority,
                    },
                )

    def _delete_duplicate_entries(self) -> None:
        if not self.entries_to_delete:
            return
        self.manifest.phase("deleting-entries")
        ordered = sorted(
            self.entries_to_delete,
            key=lambda item: 0 if item[0]["type"] == "Branch" else 1,
        )
        for source, existing_id in ordered:
            self.client.delete(f"/entries/by-id/{quote(existing_id, safe='')}")
            self.manifest.record_deleted(
                {
                    "id": existing_id,
                    "sourceId": source["id"],
                    "name": source["name"],
                    "type": source["type"],
                }
            )

    def _create_entry(self, entry: dict[str, Any], parent_id: str) -> None:
        source_id = entry["id"]
        if source_id in self.entry_ids:
            return
        payload = clean_entry(entry, parent_id)
        deferred: dict[str, Any] = {}
        if self.piecemeal:
            for field in PIECEMEAL_ENTRY_FIELDS:
                if field in payload:
                    deferred[field] = payload.pop(field)
        created_id = self.created_id(
            self.client.post("/entries", payload),
            "POST /entries",
        )
        self.entry_ids[source_id] = created_id
        self.manifest.record_created(
            "entries",
            {
                "id": created_id,
                "sourceId": source_id,
                "name": entry["name"],
                "type": entry["type"],
            },
        )
        self.manifest.map_id("entries", (source_id,), created_id)
        entry_path = f"/entries/by-id/{quote(created_id, safe='')}"
        for field, value in deferred.items():
            self.client.patch(entry_path, {field: value})
            self.manifest.record_patch(created_id, field)

    def verify(self) -> None:
        self.manifest.phase("verifying")
        for item in self.dataset.tiers:
            created_id = self.tier_ids[item.source_ids[0]]
            tier = self.client.get(f"/tiers/{quote(created_id, safe='')}")
            if (
                not isinstance(tier, dict)
                or tier.get("name") != item.name
                or tier.get("consortium") != self.consortium_id
                or (
                    self.verify_only
                    and not contains_expected(tier, item.definition)
                )
            ):
                raise LoadError(f"tier {item.name!r} failed verification")
        for item in self.dataset.networks:
            created_id = self.network_ids[item.source_ids[0]]
            network = self.client.get(f"/networks/{quote(created_id, safe='')}")
            if (
                not isinstance(network, dict)
                or network.get("name") != item.name
                or network.get("consortium") != self.consortium_id
                or (
                    self.verify_only
                    and not contains_expected(network, item.definition)
                )
            ):
                raise LoadError(f"network {item.name!r} failed verification")

        for entry in self.dataset.entries:
            source_id = entry["id"]
            created_id = self.entry_ids[source_id]
            created = self.client.get(f"/entries/by-id/{quote(created_id, safe='')}")
            expected_parent = (
                self.consortium_id
                if entry["type"] == "Institution"
                else self.entry_ids[entry["parent"]]
            )
            reused_entry = source_id in self.reused_entry_source_ids
            expected_entry = clean_entry(entry, expected_parent)
            actual_entry = (
                clean_entry(created, created.get("parent"))
                if isinstance(created, dict)
                else created
            )
            if (
                not isinstance(created, dict)
                or created.get("id") != created_id
                or (not reused_entry and created.get("name") != entry["name"])
                or created.get("type") != entry["type"]
                or created.get("parent") != expected_parent
                or (
                    self.verify_only
                    and not contains_expected(actual_entry, expected_entry)
                )
            ):
                raise LoadError(f"entry {entry['name']!r} failed verification")

            entry_path = f"/entries/by-id/{quote(created_id, safe='')}"
            tiers = self.client.list_all(f"{entry_path}/tiers")
            actual_tiers = {item.get("id") for item in tiers}
            expected_tiers = {
                self.tier_ids[item["id"]] for item in entry.get("tiers") or []
            }
            tiers_match = (
                expected_tiers <= actual_tiers
                if reused_entry
                else expected_tiers == actual_tiers
            )
            if not tiers_match:
                raise LoadError(f"entry {entry['name']!r} has incorrect tiers")

            networks = self.client.list_all(f"{entry_path}/networks")
            actual_networks = {
                item.get("id"): item.get("priority") for item in networks
            }
            expected_networks = {
                self.network_ids[item["id"]]: item.get("priority", 0)
                for item in entry.get("networks") or []
            }
            networks_match = (
                all(actual_networks.get(key) == value for key, value in expected_networks.items())
                if reused_entry
                else actual_networks == expected_networks
            )
            if not networks_match:
                raise LoadError(f"entry {entry['name']!r} has incorrect networks")


def default_result_path() -> Path:
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    return Path(f"directory-load-result-{stamp}.json")


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--base-url",
        required=True,
        help="Okapi-routed Directory base URL (the /directory suffix is optional)",
    )
    parser.add_argument(
        "--consortium-id", required=True, help="UUID of the target Consortium entry"
    )
    parser.add_argument(
        "--tenant", required=True, help="FOLIO tenant sent as X-Okapi-Tenant"
    )
    parser.add_argument(
        "--fixture",
        required=True,
        type=Path,
        help="path to the exported Directory JSON array",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="validate and check the target without creating resources",
    )
    parser.add_argument(
        "--allow-existing",
        action="store_true",
        help="reuse matching entries, tiers, networks, and memberships",
    )
    parser.add_argument(
        "--delete-entries",
        action="store_true",
        help="delete duplicate entries before loading their replacements",
    )
    parser.add_argument(
        "--verify",
        action="store_true",
        help="only verify that all fixture data is present; do not modify the server",
    )
    parser.add_argument(
        "--piecemeal",
        action="store_true",
        help="create entries first, then PATCH selected nested fields separately",
    )
    parser.add_argument(
        "--replace-localhost",
        metavar="URL",
        help="replace the origin of stored HTTP(S) localhost URLs",
    )
    parser.add_argument(
        "--result",
        type=Path,
        help="result manifest path (defaults to a timestamped file)",
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=30.0,
        help="per-request timeout in seconds (default: 30)",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    result_path = args.result or default_result_path()
    manifest: ResultManifest | None = None
    token: str | None = None
    try:
        base_url = normalize_base_url(args.base_url)
        consortium_id = require_uuid(args.consortium_id, "--consortium-id")
        tenant = require_string(args.tenant, "--tenant")
        token = require_string(os.getenv("DIRECTORY_TOKEN"), "DIRECTORY_TOKEN")
        localhost_replacement = (
            normalize_localhost_replacement(args.replace_localhost)
            if args.replace_localhost
            else None
        )
        manifest = ResultManifest(
            result_path,
            base_url,
            tenant,
            consortium_id,
            args.fixture,
            args.dry_run,
            args.allow_existing,
            args.piecemeal,
            localhost_replacement,
            args.delete_entries,
            args.verify,
        )
        manifest.write()
        if args.verify and args.delete_entries:
            raise LoadError("--verify cannot be combined with --delete-entries")
        raw_dataset = json.loads(args.fixture.read_text(encoding="utf-8"))
        replacements = 0
        if localhost_replacement:
            raw_dataset, replacements = replace_localhost_urls(
                raw_dataset, localhost_replacement
            )
            manifest.data["localhostUrlsReplaced"] = replacements
        manifest.phase("validating")
        dataset = validate_dataset(raw_dataset)
        loader = DirectoryLoader(
            DirectoryClient(base_url, token, tenant, args.timeout),
            dataset,
            consortium_id,
            manifest,
            args.allow_existing,
            args.piecemeal,
            args.delete_entries,
            args.verify,
        )
        counts = loader.preflight()
        counts["localhostUrlsReplaced"] = replacements
        print("Preflight succeeded: " + ", ".join(f"{k}={v}" for k, v in counts.items()))
        manifest.data["planned"] = counts
        if args.verify:
            loader.verify()
            manifest.finish("verified")
            print(f"Directory verification succeeded; result manifest: {result_path}")
            return 0
        if args.dry_run:
            manifest.finish("dry-run")
            print(f"Dry run succeeded; result manifest: {result_path}")
            return 0

        loader.load()
        loader.verify()
        manifest.finish("succeeded")
        print(f"Directory load and verification succeeded; result manifest: {result_path}")
        return 0
    except (OSError, json.JSONDecodeError, LoadError) as error:
        message = str(error)
        if token:
            message = message.replace(token, "[REDACTED]")
        if manifest is not None:
            try:
                manifest.finish("failed", message)
            except OSError as manifest_error:
                message += f"; could not update result manifest: {manifest_error}"
        print(f"Directory load failed: {message}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
