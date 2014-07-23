// Copyright 2014 The Chihaya Authors. All rights reserved.
// Use of this source code is governed by the BSD 2-Clause license,
// which can be found in the LICENSE file.

package http

import (
	"bytes"
	"net/http"

	"github.com/chihaya/bencode"
	"github.com/chihaya/chihaya/tracker/models"
)

// Writer implements the tracker.Writer interface for the HTTP protocol.
type Writer struct {
	http.ResponseWriter
}

// WriteError writes a bencode dict with a failure reason.
func (w *Writer) WriteError(err error) error {
	bencoder := bencode.NewEncoder(w)

	return bencoder.Encode(bencode.Dict{
		"failure reason": err.Error(),
	})
}

// WriteAnnounce writes a bencode dict representation of an AnnounceResponse.
func (w *Writer) WriteAnnounce(res *models.AnnounceResponse) error {
	dict := bencode.Dict{
		"complete":     res.Complete,
		"incomplete":   res.Incomplete,
		"interval":     res.Interval,
		"min interval": res.MinInterval,
	}

	if res.Compact {
		if res.IPv4Peers != nil {
			dict["peers"] = compactPeers(false, res.IPv4Peers)
		}
		if res.IPv6Peers != nil {
			compact := compactPeers(true, res.IPv6Peers)

			// Don't bother writing the IPv6 field if there is no value.
			if len(compact) > 0 {
				dict["peers6"] = compact
			}
		}
	} else if res.IPv4Peers != nil || res.IPv6Peers != nil {
		dict["peers"] = peersList(res.IPv4Peers, res.IPv6Peers)
	}

	bencoder := bencode.NewEncoder(w)
	return bencoder.Encode(dict)
}

// WriteScrape writes a bencode dict representation of a ScrapeResponse.
func (w *Writer) WriteScrape(res *models.ScrapeResponse) error {
	dict := bencode.Dict{
		"files": filesDict(res.Files),
	}

	bencoder := bencode.NewEncoder(w)
	return bencoder.Encode(dict)
}

func compactPeers(ipv6 bool, peers models.PeerList) []byte {
	var compactPeers bytes.Buffer

	if ipv6 {
		for _, peer := range peers {
			compactPeers.Write(peer.IPv6)
			compactPeers.Write([]byte{byte(peer.Port >> 8), byte(peer.Port & 0xff)})
		}
	} else {
		for _, peer := range peers {
			compactPeers.Write(peer.IPv4)
			compactPeers.Write([]byte{byte(peer.Port >> 8), byte(peer.Port & 0xff)})
		}
	}

	return compactPeers.Bytes()
}

func peersList(ipv4s, ipv6s models.PeerList) (peers []bencode.Dict) {
	for _, peer := range ipv4s {
		peers = append(peers, peerDict(&peer, false))
	}
	for _, peer := range ipv6s {
		peers = append(peers, peerDict(&peer, true))
	}
	return peers
}

func peerDict(peer *models.Peer, ipv6 bool) bencode.Dict {
	var ip string

	if ipv6 {
		ip = peer.IPv6.String()
	} else {
		ip = peer.IPv4.String()
	}

	return bencode.Dict{
		"ip":      ip,
		"peer id": peer.ID,
		"port":    peer.Port,
	}
}

func filesDict(torrents []*models.Torrent) bencode.Dict {
	d := bencode.NewDict()
	for _, torrent := range torrents {
		d[torrent.Infohash] = torrentDict(torrent)
	}
	return d
}

func torrentDict(torrent *models.Torrent) bencode.Dict {
	return bencode.Dict{
		"complete":   len(torrent.Seeders),
		"incomplete": len(torrent.Leechers),
		"downloaded": torrent.Snatches,
	}
}
