package profiles

// fillMissing records constructor defaults without overwriting explicit values.
func fillMissing(dst, defaults object, path string, origins map[string]string) {
	for key, value := range defaults {
		fullPath := path + "." + key
		if nested, ok := value.(object); ok {
			current, exists := dst[key].(object)
			if !exists {
				if _, present := dst[key]; present {
					continue
				}
				current = object{}
				dst[key] = current
			}
			fillMissing(current, nested, fullPath, origins)
		} else if _, exists := dst[key]; !exists {
			dst[key] = value
			origins[fullPath] = "Generic"
		}
	}
}

// materializeCatalogDefaults mirrors the established catalog constructors,
// including the legacy rule that a partial MARC mapping supplies only its
// explicitly configured fields. Vendor MARC mappings have already been merged.
func materializeCatalogDefaults(c object, origins map[string]string) {
	fillMissing(c, object{"queryConfig": object{"type": "pqf"}}, "catalogConfig", origins)
	q := c["queryConfig"].(object)
	query := object{"identifier": "@attr 1=12 {term}", "isbn": "@attr 1=7 {term}", "issn": "@attr 1=8 {term}", "title": "@attr 1=4 {term}"}
	if q["type"] == "cql" {
		query = object{"identifier": "rec.id = {term}", "isbn": "isbn = {term}", "issn": "issn = {term}", "title": "title = {term}"}
	}
	fillMissing(q, query, "catalogConfig.queryConfig", origins)
	if _, ok := c["metadataFormat"]; !ok {
		c["metadataFormat"] = object{"marc21": object{}}
	}
	if format, ok := c["metadataFormat"].(object); ok {
		if m, ok := format["marc21"].(object); ok {
			fillMissing(m, object{"identifier": "001", "title": "245$a$n$p", "subtitle": "245$b", "isbn": "020$a", "issn": "022$a", "author": "100$a/100$?/110$a/110$?/111$a/111$?/245$c", "edition": "250$a"}, "catalogConfig.metadataFormat.marc21", origins)
		}
	}
	if _, ok := c["holdingsFormat"]; !ok {
		c["holdingsFormat"] = object{"marc": object{}}
	}
	if format, ok := c["holdingsFormat"].(object); ok {
		if m, ok := format["marc"].(object); ok && len(m) == 0 {
			fillMissing(m, object{"mainField": "852", "locationSubField": "b", "shelvingLocationSubField": "c", "callNumberSubField": "h", "itemIdSubField": "p", "restrictedSubField": "r"}, "catalogConfig.holdingsFormat.marc", origins)
		}
		if o, ok := format["opac"].(object); ok {
			fillMissing(o, object{"availabilityRule": "availableNow", "requireLocalLocation": false, "shelvingLocationSource": "shelvingLocation", "includeItemId": true, "includeItemLoanPolicy": true, "includeTemporaryLocation": false, "allCirculations": false}, "catalogConfig.holdingsFormat.opac", origins)
		}
	}
}

// Diagnostics returns behavior settings only. Connection options, addresses,
// authentication, agencies, patrons and local supply policies are excluded.
func (e *Effective) Diagnostics() object {
	result := object{"lmsVendor": e.LMSVendor, "catalogProfile": e.CatalogProfile}
	if e.LMS != nil {
		raw := asObject(e.LMS)
		safe := object{}
		for _, key := range []string{"ncipNamespaceEnabled", "bibIdNormalization", "requestItemRequestType", "requestItemRequestScopeType", "requestItemBibIdCode", "requestItemPickupLocationEnabled", "lookupUserEnabled", "acceptItemEnabled", "checkInItemEnabled", "checkOutItemEnabled", "requestItemEnabled"} {
			safe[key] = raw[key]
		}
		result["lmsConfig"] = safe
	}
	if e.Catalog != nil {
		raw := asObject(e.Catalog)
		safe := object{"queryConfig": raw["queryConfig"], "metadataFormat": raw["metadataFormat"], "holdingsFormat": raw["holdingsFormat"]}
		if e.Catalog.Sru != nil {
			safe["recordSchema"] = e.Catalog.Sru.RecordSchema
		}
		if e.Catalog.Zoom != nil && e.Catalog.Zoom.Options != nil {
			safe["preferredRecordSyntax"] = (*e.Catalog.Zoom.Options)["preferredRecordSyntax"]
		}
		result["catalogConfig"] = safe
	}
	result["origins"] = e.Origins
	return result
}
