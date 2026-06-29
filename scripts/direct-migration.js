const { createClient } = require('@supabase/supabase-js');
const fs = require('fs');
const path = require('path');

// Supabase configuration from secrets
const supabaseUrl = 'https://jzzvizacfyipzdyiqfzb.supabase.co';
const supabaseServiceKey = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZSIsInJlZiI6Imp6enZpemFjZnlpcHpkeWlxZnpiIiwicm9sZSI6InNlcnZpY2Vfcm9sZSIsImlhdCI6MTc1OTY2OTM2OCwiZXhwIjoyMDc1MjQ1MzY4fQ.NF-YqGoSpFLw5T7gdHqAAwiqNKC_5efRtNcPcCj6RA8';

// Create Supabase client with service role key
const supabase = createClient(supabaseUrl, supabaseServiceKey);

async function executeMigration() {
    try {
        console.log('🔄 Starting subscription configuration migration...');

        // Read the SQL migration script
        const sqlScript = fs.readFileSync(path.join(__dirname, 'execute_subscription_migration.sql'), 'utf8');

        console.log('📝 Executing SQL migration script...');

        // Execute the SQL using Supabase's raw SQL execution
        const { data, error } = await supabase.rpc('exec_sql', { sql: sqlScript });

        if (error) {
            console.error('❌ Migration failed:', error);
            throw error;
        }

        console.log('✅ Migration completed successfully!');
        console.log('📊 Result:', data);

        // Verify tables were created
        const { data: tables, error: tablesError } = await supabase
            .from('information_schema.tables')
            .select('table_name')
            .eq('table_schema', 'public')
            .in('table_name', ['subscription_permissions', 'subscription_token_costs', 'subscription_pricing', 'subscription_config_history']);

        if (tablesError) {
            console.error('❌ Failed to verify tables:', tablesError);
        } else {
            console.log('✅ Created tables:', tables.map(t => t.table_name).join(', '));
        }

    } catch (error) {
        console.error('❌ Migration failed:', error);
        process.exit(1);
    }
}

executeMigration();