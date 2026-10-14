package testing

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

// Image pins match deployments/compose/docker-compose.yml (tag form).
const (
	postgresImage   = "postgres:18.6"
	redisImage      = "redis:8.10.2"
	clickHouseImage = "clickhouse/clickhouse-server:26.3.47.6"
	kafkaImage      = "apache/kafka:4.3.1"

	startupTimeout     = 120 * time.Second
	topicCreateTimeout = 30 * time.Second
	kafkaPort          = "29092"
	loopback           = "127.0.0.1"

	// PostgresPassword is the password of the throwaway Postgres container.
	PostgresPassword = "testpassword"
	// ClickHousePassword is the password of the throwaway ClickHouse container.
	ClickHousePassword = "testpassword"

	postgresReadyLogs = 2 // initdb restarts the server once
	postgresUser      = "sentinel"
	postgresDB        = "sentinel"
	defaultMaxConns   = 5
	defaultConnSecs   = 300
)

func start(ctx context.Context, req testcontainers.ContainerRequest) (testcontainers.Container, error) {
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		if c != nil {
			_ = c.Terminate(ctx)
		}

		return nil, fmt.Errorf("start %s: %w", req.Image, err)
	}

	return c, nil
}

func terminator(c testcontainers.Container) func() {
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
		defer cancel()

		_ = c.Terminate(ctx)
	}
}

func hostPort(ctx context.Context, c testcontainers.Container, port string) (string, int, error) {
	host, err := c.Host(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("container host: %w", err)
	}

	mapped, err := c.MappedPort(ctx, port+"/tcp")
	if err != nil {
		return "", 0, fmt.Errorf("mapped port %s: %w", port, err)
	}

	n, err := strconv.Atoi(mapped.Port())
	if err != nil {
		return "", 0, fmt.Errorf("parse port: %w", err)
	}

	return host, n, nil
}

// StartPostgres starts a throwaway PostgreSQL container.
func StartPostgres(ctx context.Context) (config.DatabaseConfig, func(), error) {
	cfg, handle, err := StartPostgresFaultContainer(ctx)
	if err != nil {
		return config.DatabaseConfig{}, nil, err
	}

	return cfg, handle.Close, nil
}

// StartPostgresFaultContainer starts an isolated PostgreSQL instance and
// retains its handle for pause/unpause failure tests.
func StartPostgresFaultContainer(ctx context.Context) (config.DatabaseConfig, *FaultContainer, error) {
	c, err := start(ctx, testcontainers.ContainerRequest{
		Image:        postgresImage,
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     postgresUser,
			"POSTGRES_DB":       postgresDB,
			"POSTGRES_PASSWORD": PostgresPassword,
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(postgresReadyLogs),
			wait.ForListeningPort("5432/tcp"),
		).WithDeadline(startupTimeout),
	})
	if err != nil {
		return config.DatabaseConfig{}, nil, err
	}
	handle := &FaultContainer{Container: c}

	host, port, err := hostPort(ctx, c, "5432")
	if err != nil {
		handle.Close()

		return config.DatabaseConfig{}, nil, err
	}

	return config.DatabaseConfig{
		Host:            host,
		Port:            port,
		User:            postgresUser,
		Password:        PostgresPassword,
		Name:            postgresDB,
		SSLMode:         "disable",
		MaxOpenConns:    defaultMaxConns,
		MaxIdleConns:    defaultMaxConns,
		ConnMaxLifetime: defaultConnSecs,
		ConnMaxIdleTime: defaultConnSecs,
	}, handle, nil
}

// StartRedis starts a throwaway Redis container.
func StartRedis(ctx context.Context) (config.RedisConfig, func(), error) {
	cfg, handle, err := StartRedisFaultContainer(ctx)
	if err != nil {
		return config.RedisConfig{}, nil, err
	}

	return cfg, handle.Close, nil
}

// StartRedisFaultContainer starts an isolated Redis instance and retains its
// handle for pause/unpause failure tests.
func StartRedisFaultContainer(ctx context.Context) (config.RedisConfig, *FaultContainer, error) {
	c, err := start(ctx, testcontainers.ContainerRequest{
		Image:        redisImage,
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor: wait.ForLog("Ready to accept connections").
			WithStartupTimeout(startupTimeout),
	})
	if err != nil {
		return config.RedisConfig{}, nil, err
	}
	handle := &FaultContainer{Container: c}

	host, port, err := hostPort(ctx, c, "6379")
	if err != nil {
		handle.Close()

		return config.RedisConfig{}, nil, err
	}

	return config.RedisConfig{Address: net.JoinHostPort(host, strconv.Itoa(port))}, handle, nil
}

// StartClickHouse starts a throwaway ClickHouse container. HTTPAddress is
// host:port with no scheme.
func StartClickHouse(ctx context.Context) (config.ClickHouseConfig, func(), error) {
	c, err := start(ctx, testcontainers.ContainerRequest{
		Image:        clickHouseImage,
		ExposedPorts: []string{"8123/tcp", "9000/tcp"},
		Env: map[string]string{
			"CLICKHOUSE_DB":                        postgresDB,
			"CLICKHOUSE_USER":                      postgresUser,
			"CLICKHOUSE_PASSWORD":                  ClickHousePassword,
			"CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT": "1",
		},
		WaitingFor: wait.ForHTTP("/ping").WithPort("8123/tcp").WithStartupTimeout(startupTimeout),
	})
	if err != nil {
		return config.ClickHouseConfig{}, nil, err
	}

	host, httpPort, err := hostPort(ctx, c, "8123")
	if err != nil {
		terminator(c)()

		return config.ClickHouseConfig{}, nil, err
	}

	_, nativePort, err := hostPort(ctx, c, "9000")
	if err != nil {
		terminator(c)()

		return config.ClickHouseConfig{}, nil, err
	}

	return config.ClickHouseConfig{
		HTTPAddress:   net.JoinHostPort(host, strconv.Itoa(httpPort)),
		NativeAddress: net.JoinHostPort(host, strconv.Itoa(nativePort)),
		User:          postgresUser,
		Password:      ClickHousePassword,
		Database:      postgresDB,
	}, terminator(c), nil
}

// FreePort returns a currently unused loopback TCP port for isolated process tests.
func FreePort() (string, error) {
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", loopback+":0")
	if err != nil {
		return "", fmt.Errorf("find free port: %w", err)
	}
	defer func() { _ = l.Close() }()

	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		return "", fmt.Errorf("split address: %w", err)
	}

	return port, nil
}

// FaultContainer retains a dedicated container handle for pause/unpause fault tests.
// Call Close as soon as the handle is acquired, normally through t.Cleanup.
type FaultContainer struct {
	testcontainers.Container
}

// Close terminates the dedicated container with a bounded background context.
func (c *FaultContainer) Close() { terminator(c.Container)() }

// Pause suspends the dedicated dependency container for bounded outage tests.
func (c *FaultContainer) Pause(ctx context.Context) error {
	return dockerContainerCommand(ctx, "pause", c.GetContainerID())
}

// Unpause resumes a container previously paused by Pause.
func (c *FaultContainer) Unpause(ctx context.Context) error {
	return dockerContainerCommand(ctx, "unpause", c.GetContainerID())
}

func dockerContainerCommand(ctx context.Context, action, id string) error {
	if id == "" {
		return errors.New("container id is empty")
	}
	if action != "pause" && action != "unpause" {
		return fmt.Errorf("unsupported container action %q", action)
	}
	// action is allowlisted and id comes from testcontainers, never user input.
	//nolint:gosec // exec uses fixed docker argv without a shell
	if out, err := exec.CommandContext(ctx, "docker", action, id).CombinedOutput(); err != nil {
		return fmt.Errorf("docker %s container: %w: %s", action, err, strings.TrimSpace(string(out)))
	}

	return nil
}

// StartKafka starts a single-node KRaft apache/kafka container whose external
// listener is advertised on a fixed free host port.
func StartKafka(ctx context.Context) (config.KafkaConfig, func(), error) {
	cfg, handle, err := StartKafkaFaultContainer(ctx)
	if err != nil {
		return config.KafkaConfig{}, nil, err
	}

	return cfg, handle.Close, nil
}

// StartClickHouseFaultContainer starts an isolated ClickHouse instance and
// retains its handle for pause/unpause failure tests.
func StartClickHouseFaultContainer(ctx context.Context) (config.ClickHouseConfig, *FaultContainer, error) {
	c, err := start(ctx, testcontainers.ContainerRequest{
		Image:        clickHouseImage,
		ExposedPorts: []string{"8123/tcp", "9000/tcp"},
		Env: map[string]string{
			"CLICKHOUSE_DB":                        postgresDB,
			"CLICKHOUSE_USER":                      postgresUser,
			"CLICKHOUSE_PASSWORD":                  ClickHousePassword,
			"CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT": "1",
		},
		WaitingFor: wait.ForHTTP("/ping").WithPort("8123/tcp").WithStartupTimeout(startupTimeout),
	})
	if err != nil {
		return config.ClickHouseConfig{}, nil, err
	}
	handle := &FaultContainer{Container: c}

	host, httpPort, err := hostPort(ctx, c, "8123")
	if err != nil {
		handle.Close()

		return config.ClickHouseConfig{}, nil, err
	}
	_, nativePort, err := hostPort(ctx, c, "9000")
	if err != nil {
		handle.Close()

		return config.ClickHouseConfig{}, nil, err
	}

	return config.ClickHouseConfig{
		HTTPAddress:   net.JoinHostPort(host, strconv.Itoa(httpPort)),
		NativeAddress: net.JoinHostPort(host, strconv.Itoa(nativePort)),
		User:          postgresUser,
		Password:      ClickHousePassword,
		Database:      postgresDB,
	}, handle, nil
}

// StartKafkaFaultContainer starts an isolated broker and returns its handle.
// Fault tests must use this instead of pausing the package-wide TestMain broker.
func StartKafkaFaultContainer(ctx context.Context) (config.KafkaConfig, *FaultContainer, error) {
	port, err := FreePort()
	if err != nil {
		return config.KafkaConfig{}, nil, err
	}

	c, err := start(ctx, testcontainers.ContainerRequest{
		Image:        kafkaImage,
		ExposedPorts: []string{kafkaPort + "/tcp"},
		Env: map[string]string{
			"CLUSTER_ID":                     "MkU3OEVBNTcwNTJENDM2Qk",
			"KAFKA_NODE_ID":                  "1",
			"KAFKA_PROCESS_ROLES":            "broker,controller",
			"KAFKA_CONTROLLER_QUORUM_VOTERS": "1@localhost:9093",
			"KAFKA_LISTENERS":                "INTERNAL://:9092,EXTERNAL://:29092,CONTROLLER://:9093",
			"KAFKA_ADVERTISED_LISTENERS":     "INTERNAL://localhost:9092,EXTERNAL://localhost:" + port,

			"KAFKA_LISTENER_SECURITY_PROTOCOL_MAP": "INTERNAL:PLAINTEXT,EXTERNAL:PLAINTEXT,CONTROLLER:PLAINTEXT",

			"KAFKA_INTER_BROKER_LISTENER_NAME":                       "INTERNAL",
			"KAFKA_CONTROLLER_LISTENER_NAMES":                        "CONTROLLER",
			"KAFKA_AUTO_CREATE_TOPICS_ENABLE":                        "false",
			"KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR":                 "1",
			"KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR":         "1",
			"KAFKA_TRANSACTION_STATE_LOG_MIN_ISR":                    "1",
			"KAFKA_SHARE_COORDINATOR_STATE_TOPIC_REPLICATION_FACTOR": "1",
			"KAFKA_SHARE_COORDINATOR_STATE_TOPIC_MIN_ISR":            "1",
			"KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS":                 "0",
		},
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.PortBindings = network.PortMap{
				network.MustParsePort(kafkaPort + "/tcp"): []network.PortBinding{
					{HostIP: netip.MustParseAddr(loopback), HostPort: port},
				},
			}
		},
		WaitingFor: wait.ForLog("Kafka Server started").WithStartupTimeout(startupTimeout),
	})
	if err != nil {
		return config.KafkaConfig{}, nil, err
	}

	return config.KafkaConfig{Brokers: net.JoinHostPort("localhost", port)}, &FaultContainer{Container: c}, nil
}

// CreateUniqueTopic creates an explicitly named one-partition topic. Kafka
// auto-creation stays disabled, so tests cannot silently share topic state.
func CreateUniqueTopic(ctx context.Context, cfg config.KafkaConfig, prefix string) (string, error) {
	brokers := strings.Split(cfg.Brokers, ",")
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return "", fmt.Errorf("create kafka admin client: %w", err)
	}
	defer client.Close()

	topic := prefix + "-" + uuid.NewString()
	if _, err = kadm.NewClient(client).CreateTopic(ctx, 1, 1, nil, topic); err != nil {
		return "", fmt.Errorf("create topic %s: %w", topic, err)
	}

	return topic, nil
}

// RequireUniqueTopic creates a unique topic or fails the current test.
func RequireUniqueTopic(t *testing.T, cfg config.KafkaConfig, prefix string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), topicCreateTimeout)
	t.Cleanup(cancel)
	topic, err := CreateUniqueTopic(ctx, cfg, prefix)
	if err != nil {
		t.Fatalf("create unique Kafka topic: %v", err)
	}

	return topic
}

func mustStart(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("container start failed: %v", err)
	}
}

// RequirePostgres starts Postgres for one test and terminates it on cleanup.
func RequirePostgres(t *testing.T) config.DatabaseConfig {
	t.Helper()

	cfg, stop, err := StartPostgres(context.Background())
	mustStart(t, err)
	t.Cleanup(stop)

	return cfg
}

// RequireRedis starts Redis for one test and terminates it on cleanup.
func RequireRedis(t *testing.T) config.RedisConfig {
	t.Helper()

	cfg, stop, err := StartRedis(context.Background())
	mustStart(t, err)
	t.Cleanup(stop)

	return cfg
}

// RequireClickHouse starts ClickHouse for one test and terminates it on cleanup.
func RequireClickHouse(t *testing.T) config.ClickHouseConfig {
	t.Helper()

	cfg, stop, err := StartClickHouse(context.Background())
	mustStart(t, err)
	t.Cleanup(stop)

	return cfg
}

// RequireKafka starts Kafka for one test and terminates it on cleanup.
func RequireKafka(t *testing.T) config.KafkaConfig {
	t.Helper()

	cfg, stop, err := StartKafka(context.Background())
	mustStart(t, err)
	t.Cleanup(stop)

	return cfg
}
