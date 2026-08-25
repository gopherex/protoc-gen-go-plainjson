package bench_test

import (
	"time"

	pb "github.com/gopherex/protoc-gen-go-plainjson/example/bench"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// observedAt is fixed so benchmark input never varies between runs.
var observedAt = timestamppb.New(time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC))

// newScalars builds the flat scalar message.
func newScalars() *pb.Scalars {
	return &pb.Scalars{
		Id:        "s-1",
		Pid:       4242,
		BytesRead: 1 << 40,
		Offset:    9007199254740993,
		Load:      1.75,
		Active:    true,
		Digest:    []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		Severity:  pb.Severity_SEVERITY_HIGH,
		Observed:  observedAt,
		Took:      durationpb.New(1500 * time.Millisecond),
	}
}

// newEvent builds the full telemetry event: five levels, a oneof, a map, a
// repeated field and well-known types.
func newEvent() *pb.Event {
	return &pb.Event{
		Id:       "e-1",
		Observed: observedAt,
		Process: &pb.Process{
			Exe: &pb.Exe{
				Path:   "/usr/bin/curl",
				Sha256: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				Owner:  &pb.User{Name: "ann", Uid: 1000},
			},
			Os: &pb.Process_Linux{Linux: &pb.Linux{
				Pid:    4242,
				Cgroup: &pb.Cgroup{Path: "/sys/fs/cgroup/app.slice"},
				Ucred:  &pb.Ucred{Uid: 1000, Gid: 1000},
			}},
			Argv: []string{"curl", "-sS", "--fail", "https://example.com/v1/items"},
		},
		Network: &pb.Network{
			RemoteIp:   "10.0.0.1",
			RemotePort: 443,
			Peer:       &pb.Peer{Host: "example.com", Asn: "AS15169"},
		},
		Labels: map[string]string{"env": "prod", "region": "eu-central-1", "tier": "edge"},
		Debug:  &pb.Debug{Trace: "0af7651916cd43dd8448eb211c80319c", Frames: []string{"a", "b"}},
	}
}

// newEventPlain is the same data in the shape a hand-written flattener can
// reproduce exactly.
func newEventPlain() *pb.EventPlain {
	return &pb.EventPlain{
		Id:       "e-1",
		Observed: observedAt,
		Process: &pb.ProcessPlain{
			Exe: &pb.ExePlain{
				ExePath: "/usr/bin/curl",
				Sha256:  "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				Owner:   &pb.UserPlain{Name: "ann", Uid: 1000},
			},
			Linux: &pb.LinuxPlain{
				Pid:    4242,
				Cgroup: &pb.Cgroup{Path: "/sys/fs/cgroup/app.slice"},
				Ucred:  &pb.Ucred{Uid: 1000, Gid: 1000},
			},
			Argv: []string{"curl", "-sS", "--fail", "https://example.com/v1/items"},
		},
		Network: &pb.Network{
			RemoteIp:   "10.0.0.1",
			RemotePort: 443,
			Peer:       &pb.Peer{Host: "example.com", Asn: "AS15169"},
		},
	}
}

// newCollideParts builds the two subtrees used to price each collision policy.
// Both claim "path", so every policy actually has work to do.
func newCollideParts() (*pb.PathA, *pb.PathB) {
	return &pb.PathA{Path: "/sys/fs/cgroup/app.slice", Owner: "ann", Mode: 0o644},
		&pb.PathB{
			Path:   "/usr/bin/curl",
			Sha256: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			Size:   184992,
		}
}
