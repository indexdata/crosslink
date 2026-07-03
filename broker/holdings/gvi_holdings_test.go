package holdings

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/directory"
	"github.com/stretchr/testify/assert"
)

const GviResponse = `<?xml version="1.0" ?>
  <zs:searchRetrieveResponse xmlns:zs="http://www.loc.gov/zing/srw/">
    <zs:version>1.1</zs:version>
    <zs:numberOfRecords>1</zs:numberOfRecords>
    <zs:records>
      <zs:record>
        <zs:recordSchema>marcxml</zs:recordSchema>
        <zs:recordPacking>xml</zs:recordPacking>
        <zs:recordData>
<record>
  <leader>06947cam a2200961 c 4500</leader>
  <controlfield tag="001">1795329181</controlfield>
  <controlfield tag="003">DE-627</controlfield>
  <controlfield tag="005">20251226201436.0</controlfield>
  <controlfield tag="007">cr uuu---uuuuu</controlfield>
  <controlfield tag="008">220311s2022    gw |||||o     00| ||fre c</controlfield>
  <datafield ind1=" " ind2=" " tag="020">
    <subfield code="a">9783428585014</subfield>
    <subfield code="9">978-3-428-58501-4</subfield>
  </datafield>
  <datafield ind1="7" ind2=" " tag="024">
    <subfield code="a">10.3790/978-3-428-58501-4</subfield>
    <subfield code="2">doi</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="035">
    <subfield code="a">(DE-627)1795329181</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="035">
    <subfield code="a">(DE-599)KEP076913368</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="035">
    <subfield code="a">(OCoLC)1304799781</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="035">
    <subfield code="a">(DUH)9783428585014</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="035">
    <subfield code="a">(DE-627-1)076913368</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="040">
    <subfield code="a">DE-627</subfield>
    <subfield code="b">ger</subfield>
    <subfield code="c">DE-627</subfield>
    <subfield code="e">rda</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="041">
    <subfield code="a">fre</subfield>
    <subfield code="a">ger</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="044">
    <subfield code="c">XA-DE-BE</subfield>
  </datafield>
  <datafield ind1=" " ind2="0" tag="050">
    <subfield code="a">BF531</subfield>
  </datafield>
  <datafield ind1=" " ind2="7" tag="072">
    <subfield code="a">BF</subfield>
    <subfield code="2">lcco</subfield>
  </datafield>
  <datafield ind1=" " ind2="7" tag="072">
    <subfield code="a">B</subfield>
    <subfield code="2">lcco</subfield>
  </datafield>
  <datafield ind1="0" ind2=" " tag="082">
    <subfield code="a">128.37</subfield>
    <subfield code="q">SEPA</subfield>
  </datafield>
  <datafield ind1="0" ind2=" " tag="082">
    <subfield code="a">152.4</subfield>
    <subfield code="q">SEPA</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="084">
    <subfield code="a">CV 2500</subfield>
    <subfield code="q">DE-Ofb1/22</subfield>
    <subfield code="2">rvk</subfield>
    <subfield code="0">(DE-625)rvk/19153:</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="084">
    <subfield code="a">CP 3200</subfield>
    <subfield code="q">DE-Ofb1/22</subfield>
    <subfield code="2">rvk</subfield>
    <subfield code="0">(DE-625)rvk/18977:</subfield>
  </datafield>
  <datafield ind1="0" ind2="4" tag="245">
    <subfield code="a">Les émotions créatives</subfield>
    <subfield code="c">sous la direction de Damien Ehrhardt, Hélène Fleury, Soraya Nour Sckell</subfield>
  </datafield>
  <datafield ind1=" " ind2="1" tag="264">
    <subfield code="a">Berlin</subfield>
    <subfield code="b">Duncker &amp; Humblot</subfield>
    <subfield code="c">[2022]</subfield>
  </datafield>
  <datafield ind1=" " ind2="4" tag="264">
    <subfield code="c">© 2022</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="300">
    <subfield code="a">1 Online-Ressource (225 Seiten)</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="336">
    <subfield code="a">Text</subfield>
    <subfield code="b">txt</subfield>
    <subfield code="2">rdacontent</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="337">
    <subfield code="a">Computermedien</subfield>
    <subfield code="b">c</subfield>
    <subfield code="2">rdamedia</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="338">
    <subfield code="a">Online-Ressource</subfield>
    <subfield code="b">cr</subfield>
    <subfield code="2">rdacarrier</subfield>
  </datafield>
  <datafield ind1="1" ind2=" " tag="490">
    <subfield code="a">Beiträge zur politischen Wissenschaft</subfield>
    <subfield code="v">Band 199</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="500">
    <subfield code="a">Online resource; title from title screen (viewed March 9, 2022)</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="520">
    <subfield code="a">L’importance du rôle des émotions dans la connaissance conduit à voir en elles bien davantage qu’un facteur perturbateur. Leur pertinence cognitive, de plus en plus reconnue par les sciences (naturelles, sociales, humaines…), consacre l’importance d’un tournant émotionnel (emotional turn). Les émotions constituent aussi de puissants moteurs de créativité et d’innovation, cruciaux dans la construction des formations socioculturelles. Les textes rassemblés dans le présent volume, dans une perspective résolument interdisciplinaire, traitent d’émotions puissamment agissantes dans l’existence, à la convergence des échelles individuelle et collective. Les deux premières parties s’interrogent sur la spécificité des émotions humainement vécues dans leurs interactions expérimentées via le corps et la raison. Les deux dernières parties abordent les émotions à une plus large échelle : celle des champs culturel et politique. / »Creative Emotions«: The scientific recognition of the cognitive significance of emotions confirms the importance of the emotional turn. Beyond this cognitive dimension, emotions are also motors of creativity, and crucial in the construction of socio-cultural configurations. The interdisciplinary texts gathered in this volume analyse how emotions act in the existence between individual and collective scales. They question the emotions in their interactions via the body and the reason, as well as in the cultural and political fields.</subfield>
  </datafield>
  <datafield ind1=" " ind2="0" tag="650">
    <subfield code="a">Emotions</subfield>
    <subfield code="v">Congresses</subfield>
    <subfield code="2">DLC</subfield>
  </datafield>
  <datafield ind1=" " ind2="0" tag="650">
    <subfield code="a">Emotions (Philosophy)</subfield>
    <subfield code="v">Congresses</subfield>
    <subfield code="2">DLC</subfield>
  </datafield>
  <datafield ind1=" " ind2="0" tag="650">
    <subfield code="a">Emotions and cognition</subfield>
    <subfield code="v">Congresses</subfield>
    <subfield code="2">DLC</subfield>
  </datafield>
  <datafield ind1=" " ind2="0" tag="650">
    <subfield code="a">Creation (Literary, artistic, etc.)</subfield>
    <subfield code="v">Congresses</subfield>
    <subfield code="2">DLC</subfield>
  </datafield>
  <datafield ind1=" " ind2="4" tag="650">
    <subfield code="a">Émotions (Philosophie) - Congrès</subfield>
  </datafield>
  <datafield ind1=" " ind2="4" tag="650">
    <subfield code="a">Émotions et cognition - Congrès</subfield>
  </datafield>
  <datafield ind1=" " ind2="4" tag="650">
    <subfield code="a">Creation (Literary, artistic, etc.)</subfield>
  </datafield>
  <datafield ind1=" " ind2="4" tag="650">
    <subfield code="a">Emotions</subfield>
  </datafield>
  <datafield ind1=" " ind2="4" tag="650">
    <subfield code="a">Emotions and cognition</subfield>
  </datafield>
  <datafield ind1=" " ind2="4" tag="650">
    <subfield code="a">Emotions (Philosophy)</subfield>
  </datafield>
  <datafield ind1=" " ind2="4" tag="650">
    <subfield code="a">Conference papers and proceedings</subfield>
  </datafield>
  <datafield ind1=" " ind2="4" tag="650">
    <subfield code="a">Emotionen</subfield>
  </datafield>
  <datafield ind1=" " ind2="4" tag="650">
    <subfield code="a">Kreativität</subfield>
  </datafield>
  <datafield ind1=" " ind2="4" tag="650">
    <subfield code="a">Wissensbildung</subfield>
  </datafield>
  <datafield ind1="0" ind2="0" tag="689">
    <subfield code="D">s</subfield>
    <subfield code="0">(DE-588)4138031-9</subfield>
    <subfield code="0">(DE-627)105645575</subfield>
    <subfield code="0">(DE-576)209682647</subfield>
    <subfield code="a">Ideengeschichte</subfield>
    <subfield code="2">gnd</subfield>
  </datafield>
  <datafield ind1="0" ind2="1" tag="689">
    <subfield code="D">s</subfield>
    <subfield code="0">(DE-588)4019702-5</subfield>
    <subfield code="0">(DE-627)106320602</subfield>
    <subfield code="0">(DE-576)208930418</subfield>
    <subfield code="a">Gefühl</subfield>
    <subfield code="2">gnd</subfield>
  </datafield>
  <datafield ind1="0" ind2="2" tag="689">
    <subfield code="D">s</subfield>
    <subfield code="0">(DE-588)4032903-3</subfield>
    <subfield code="0">(DE-627)106259733</subfield>
    <subfield code="0">(DE-576)208999248</subfield>
    <subfield code="a">Kreativität</subfield>
    <subfield code="2">gnd</subfield>
  </datafield>
  <datafield ind1="0" ind2="3" tag="689">
    <subfield code="D">s</subfield>
    <subfield code="0">(DE-588)4073586-2</subfield>
    <subfield code="0">(DE-627)106092022</subfield>
    <subfield code="0">(DE-576)209190922</subfield>
    <subfield code="a">Kognitive Psychologie</subfield>
    <subfield code="2">gnd</subfield>
  </datafield>
  <datafield ind1="0" ind2=" " tag="689">
    <subfield code="5">(DE-627)</subfield>
  </datafield>
  <datafield ind1="1" ind2=" " tag="700">
    <subfield code="a">Ehrhardt, Damien</subfield>
    <subfield code="d">1969-</subfield>
    <subfield code="e">HerausgeberIn</subfield>
    <subfield code="0">(DE-588)136878849</subfield>
    <subfield code="0">(DE-627)588319880</subfield>
    <subfield code="0">(DE-576)301326533</subfield>
    <subfield code="4">edt</subfield>
  </datafield>
  <datafield ind1="1" ind2=" " tag="700">
    <subfield code="a">Fleury, Hélène</subfield>
    <subfield code="e">HerausgeberIn</subfield>
    <subfield code="0">(DE-588)1252713592</subfield>
    <subfield code="0">(DE-627)1794174060</subfield>
    <subfield code="4">edt</subfield>
  </datafield>
  <datafield ind1="1" ind2=" " tag="700">
    <subfield code="a">Nour, Soraya</subfield>
    <subfield code="e">HerausgeberIn</subfield>
    <subfield code="0">(DE-588)1018682007</subfield>
    <subfield code="0">(DE-627)682873136</subfield>
    <subfield code="0">(DE-576)356209547</subfield>
    <subfield code="4">edt</subfield>
  </datafield>
  <datafield ind1="1" ind2=" " tag="776">
    <subfield code="z">9783428185016</subfield>
  </datafield>
  <datafield ind1="0" ind2="8" tag="776">
    <subfield code="i">Erscheint auch als</subfield>
    <subfield code="n">Druck-Ausgabe</subfield>
    <subfield code="t">Les émotions créatives</subfield>
    <subfield code="d">Berlin : Duncker &amp; Humblot, 2022</subfield>
    <subfield code="h">225 Seiten</subfield>
    <subfield code="w">(DE-627)1795172681</subfield>
    <subfield code="z">9783428185016</subfield>
    <subfield code="z">3428185013</subfield>
  </datafield>
  <datafield ind1=" " ind2="0" tag="830">
    <subfield code="a">Beiträge zur politischen Wissenschaft</subfield>
    <subfield code="v">Band 199</subfield>
    <subfield code="9">199</subfield>
    <subfield code="w">(DE-627)670631469</subfield>
    <subfield code="w">(DE-576)47762409X</subfield>
    <subfield code="w">(DE-600)2633572-4</subfield>
    <subfield code="7">am</subfield>
  </datafield>
  <datafield ind1="4" ind2="0" tag="856">
    <subfield code="u">https://elibrary.duncker-humblot.com/9783428585014</subfield>
    <subfield code="m">X:DUH</subfield>
    <subfield code="x">Verlag</subfield>
    <subfield code="z">lizenzpflichtig</subfield>
    <subfield code="7">1</subfield>
  </datafield>
  <datafield ind1="4" ind2="0" tag="856">
    <subfield code="u">https://doi.org/10.3790/978-3-428-58501-4</subfield>
    <subfield code="m">X:DUH</subfield>
    <subfield code="x">Resolving-System</subfield>
    <subfield code="z">lizenzpflichtig</subfield>
    <subfield code="7">1</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="912">
    <subfield code="a">ZDB-54-DHE</subfield>
    <subfield code="b">2022</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="912">
    <subfield code="a">ZDB-54-DHP</subfield>
    <subfield code="b">2022</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="912">
    <subfield code="a">ZDB-54-DKPW</subfield>
  </datafield>
  <datafield ind1="r" ind2="v" tag="936">
    <subfield code="a">CV 2500</subfield>
    <subfield code="b">Soziale Kognition</subfield>
    <subfield code="k">Psychologie</subfield>
    <subfield code="k">Sozialpsychologie</subfield>
    <subfield code="k">Soziale Kognition</subfield>
    <subfield code="0">(DE-627)1437673430</subfield>
    <subfield code="0">(DE-625)rvk/19153:</subfield>
    <subfield code="0">(DE-576)367673436</subfield>
  </datafield>
  <datafield ind1="r" ind2="v" tag="936">
    <subfield code="a">CP 3200</subfield>
    <subfield code="b">Gefühl</subfield>
    <subfield code="k">Psychologie</subfield>
    <subfield code="k">Allgemeine Psychologie</subfield>
    <subfield code="k">Gefühl</subfield>
    <subfield code="0">(DE-627)1271512165</subfield>
    <subfield code="0">(DE-625)rvk/18977:</subfield>
    <subfield code="0">(DE-576)201512165</subfield>
  </datafield>
  <datafield ind1=" " ind2=" " tag="951">
    <subfield code="a">BO</subfield>
  </datafield>
  <datafield ind1="1" ind2=" " tag="924">
    <subfield code="a">4088716612</subfield>
    <subfield code="b">DE-705</subfield>
    <subfield code="9">705</subfield>
    <subfield code="c">GBV</subfield>
    <subfield code="d">b</subfield>
    <subfield code="e">p</subfield>
    <subfield code="k">https://doi.org/10.3790/978-3-428-58501-4</subfield>
  </datafield>
  <datafield ind1="1" ind2=" " tag="924">
    <subfield code="a">4087786013</subfield>
    <subfield code="b">DE-21</subfield>
    <subfield code="9">21</subfield>
    <subfield code="c">BSZ</subfield>
    <subfield code="d">b</subfield>
    <subfield code="e">p</subfield>
    <subfield code="k">https://doi.org/10.3790/978-3-428-58501-4</subfield>
    <subfield code="l">Zugang für die Universität Tübingen</subfield>
  </datafield>
  <datafield ind1="1" ind2=" " tag="924">
    <subfield code="a">4142515608</subfield>
    <subfield code="b">DE-24</subfield>
    <subfield code="9">24</subfield>
    <subfield code="c">BSZ</subfield>
    <subfield code="d">c</subfield>
    <subfield code="k">http://han.wlb-stuttgart.de/han/dunckerhumblot-eB/elibrary.duncker-humblot.com/9783428585014/U1</subfield>
    <subfield code="1">ZDB-54-DHP</subfield>
  </datafield>
  <datafield ind1="1" ind2=" " tag="924">
    <subfield code="a">4252867134</subfield>
    <subfield code="b">DE-180</subfield>
    <subfield code="9">180</subfield>
    <subfield code="c">BSZ</subfield>
    <subfield code="d">b</subfield>
    <subfield code="k">http://primo-49man.hosted.exlibrisgroup.com/openurl/MAN/MAN_UB_service_page?u.ignore_date_coverage=true&amp;rft.mms_id=9919356436502561</subfield>
    <subfield code="l">BSO</subfield>
  </datafield>
  <datafield ind1="1" ind2=" " tag="924">
    <subfield code="a">4117933825</subfield>
    <subfield code="b">DE-Ofb1</subfield>
    <subfield code="9">Ofb 1</subfield>
    <subfield code="c">BSZ</subfield>
    <subfield code="d">b</subfield>
    <subfield code="g">E-Book Duncker &amp; Humblot</subfield>
    <subfield code="k">https://doi.org/10.3790/978-3-428-58501-4</subfield>
    <subfield code="l">Zum Online-Dokument</subfield>
    <subfield code="l">Zugang im Hochschulnetz der HS Offenburg / extern via VPN oder Shibboleth (Login über Institution)</subfield>
  </datafield>
</record>

        </zs:recordData>
      <zs:recordPosition>1</zs:recordPosition>
    </zs:record>
  </zs:records>
  <zs:echoedSearchRetrieveRequest>
    <zs:version>1.1</zs:version>
    <zs:query>rec.id="(DE-627)1795329181"</zs:query>
    <zs:startRecord>1</zs:startRecord>
    <zs:maximumRecords>1</zs:maximumRecords>
    <zs:recordPacking>xml</zs:recordPacking>
    <zs:recordSchema>marcxml</zs:recordSchema>
  </zs:echoedSearchRetrieveRequest>
  <!-- Debug Info
rec_id:   (DE-627)1795329181
year:     None
edition:  None
  -->
</zs:searchRetrieveResponse>`

func TestGviHoldings(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusOK)
		output := []byte(GviResponse)
		_, err := w.Write(output)
		assert.NoError(t, err)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	creator := NewAvailabilityCreator(AvailabilityAdapterZoom, "")

	qtype := directory.Cql
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			HoldingsConfig: &directory.HoldingsConfig{
				Zoom: &directory.ZoomConfig{
					Address: server.URL,
					Options: &map[string]string{
						"sru":         "get",
						"sru_version": "1.1",
					},
				},
				QueryConfig: &directory.QueryConfig{
					Type:       &qtype,
					Identifier: NewString("rec.id = {term}"),
				},
				ParserConfig: &directory.ParserConfig{
					Marc21plus1: &map[string]interface{}{},
				},
			},
		},
	}

	aa, err := creator.GetAdapter(peer)
	if cgoEnabled() {
		assert.NoError(t, err)
		assert.NotNil(t, aa)

		params := LookupParams{
			ServiceType: "Loan",
			Identifier:  "(DE-627)1795329181",
		}
		holdingsList, _, err := aa.HoldingsLookup(params)
		assert.NoError(t, err)
		assert.NotNil(t, holdingsList)
		assert.Len(t, holdingsList, 1)
	} else {
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cgo is not enabled")
	}
}
