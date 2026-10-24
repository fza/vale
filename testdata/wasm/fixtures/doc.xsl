<?xml version="1.0"?>
<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="html"/>
  <xsl:template match="/doc">
    <html><body>
      <h1><xsl:value-of select="title"/></h1>
      <xsl:for-each select="para"><p><xsl:value-of select="."/></p></xsl:for-each>
      <pre><xsl:value-of select="code"/></pre>
    </body></html>
  </xsl:template>
</xsl:stylesheet>
