package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/javi11/altmount/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh/terminal"
)

func init() {
	passwdCmd := &cobra.Command{
		Use:   "passwd [username]",
		Short: "Reset a user's password",
		Long:  `Reset the password for an existing user interactively. Default user is 'admin'.`,
		Args:  cobra.MaximumNArgs(1),
		RunE:  runPasswd,
	}

	rootCmd.AddCommand(passwdCmd)
}

func runPasswd(cmd *cobra.Command, args []string) error {
	username := "admin"
	if len(args) > 0 {
		username = args[0]
	}

	// 1. Load config to find database path
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config from %s: %w", configFile, err)
	}

	// 2. Initialize database connection
	ctx := context.Background()
	// initializeDatabase is internal to cmd package (defined in setup.go)
	db, err := initializeDatabase(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer db.Close()

	// 3. Setup user repository
	// setupRepositories is internal to cmd package (defined in setup.go)
	repos := setupRepositories(ctx, db)
	userRepo := repos.UserRepo

	// 4. Verify user exists
	// GetUserByUsername checks specifically for 'direct' provider users
	user, err := userRepo.GetUserByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("error looking up user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user %q not found or not a direct login user", username)
	}

	// 5. Interactive Password Prompt
	fmt.Printf("Enter new password for %s: ", username)
	bytePassword, err := terminal.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("\nfailed to read password: %w", err)
	}
	fmt.Println() // Print newline after hidden input

	fmt.Print("Confirm new password: ")
	byteConfirm, err := terminal.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("\nfailed to read confirmation: %w", err)
	}
	fmt.Println() // Print newline after hidden input

	password := string(bytePassword)
	confirm := string(byteConfirm)

	if password != confirm {
		return fmt.Errorf("passwords do not match")
	}

	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters long")
	}

	// 6. Hash Password
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// 7. Update Database
	if err := userRepo.UpdatePassword(ctx, user.UserID, string(hash)); err != nil {
		return fmt.Errorf("failed to update password in database: %w", err)
	}

	fmt.Printf("Successfully updated password for user %q.\n", username)
	return nil
}
